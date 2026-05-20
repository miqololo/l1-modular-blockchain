// Package state owns the chain's state machine. Everything that mutates state
// goes through these methods so that:
//   1) transactions can be validated and applied atomically inside a tx,
//   2) events are emitted from a single place per state change,
//   3) Phase 1 can swap bbolt for the Cosmos SDK store with one diff.
package state

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"go.etcd.io/bbolt"
	"go.uber.org/zap"

	"github.com/aios/aios/internal/types"
)

// Bucket names.
var (
	bktMeta     = []byte("meta")
	bktAccounts = []byte("accounts")
	bktServices = []byte("services")
	bktSvcByName = []byte("svc_by_name")
	bktRequests = []byte("requests")
	bktNonces   = []byte("nonces")
	bktDomains  = []byte("domains") // Phase 1
)

var (
	keyChainID      = []byte("chain_id")
	keyHeight       = []byte("height")
	keyNextSvcID    = []byte("next_service_id")
	keyNextReqID    = []byte("next_request_id")
	keyNextDomainID = []byte("next_domain_id") // Phase 1
	keyParams       = []byte("params")
	keyAuthority    = []byte("authority") // Phase 1: address allowed to call MsgRegisterDomain
)

// State is the chain's whole-world state, plus event bus.
type State struct {
	db     *bbolt.DB
	logger *zap.Logger

	mu       sync.RWMutex
	subs     map[chan types.Event]struct{}
	eventLog []types.Event // bounded — last 1024 events for replay

	chainID string
}

const eventLogLimit = 1024

// Open opens or creates the state database.
func Open(path string, logger *zap.Logger) (*State, error) {
	db, err := bbolt.Open(path, 0o600, &bbolt.Options{Timeout: 5 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	s := &State{db: db, logger: logger, subs: make(map[chan types.Event]struct{})}

	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *State) Close() error { return s.db.Close() }

func (s *State) ChainID() string { return s.chainID }

func (s *State) init() error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		for _, name := range [][]byte{bktMeta, bktAccounts, bktServices, bktSvcByName, bktRequests, bktNonces, bktDomains} {
			if _, err := tx.CreateBucketIfNotExists(name); err != nil {
				return err
			}
		}
		meta := tx.Bucket(bktMeta)
		if meta.Get(keyChainID) == nil {
			s.chainID = "aios-devnet-1"
			if err := meta.Put(keyChainID, []byte(s.chainID)); err != nil {
				return err
			}
		} else {
			s.chainID = string(meta.Get(keyChainID))
		}
		if meta.Get(keyHeight) == nil {
			if err := putUint64(meta, keyHeight, 0); err != nil {
				return err
			}
		}
		if meta.Get(keyNextSvcID) == nil {
			if err := putUint64(meta, keyNextSvcID, 1); err != nil {
				return err
			}
		}
		if meta.Get(keyNextReqID) == nil {
			if err := putUint64(meta, keyNextReqID, 1); err != nil {
				return err
			}
		}
		if meta.Get(keyNextDomainID) == nil {
			if err := putUint64(meta, keyNextDomainID, 1); err != nil {
				return err
			}
		}
		if meta.Get(keyParams) == nil {
			p, _ := json.Marshal(types.DefaultParams())
			if err := meta.Put(keyParams, p); err != nil {
				return err
			}
		}
		return nil
	})
}

// Height returns the current block height.
func (s *State) Height() int64 {
	var h int64
	_ = s.db.View(func(tx *bbolt.Tx) error {
		h = int64(getUint64(tx.Bucket(bktMeta), keyHeight))
		return nil
	})
	return h
}

// Params returns the current parameters.
func (s *State) Params() (types.Params, error) {
	var p types.Params
	err := s.db.View(func(tx *bbolt.Tx) error {
		bz := tx.Bucket(bktMeta).Get(keyParams)
		return json.Unmarshal(bz, &p)
	})
	return p, err
}

// SetParams overwrites the parameter set. Intended for genesis configuration
// and tests. Phase 4 governance will route through a proper MsgUpdateParams
// rather than a state-level setter.
func (s *State) SetParams(p types.Params) error {
	bz, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bktMeta).Put(keyParams, bz)
	})
}

// Mint credits `amount` to `addr`. Intended for genesis allocations and tests.
// No supply-cap or authorization check — Phase 4 governance gates inflation.
func (s *State) Mint(addr string, amount uint64) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return credit(tx, addr, amount)
	})
}

// ─── Accounts & balances ────────────────────────────────────────────────────

type Account struct {
	Address string `json:"address"`
	Balance uint64 `json:"balance"`
	Nonce   uint64 `json:"nonce"`
}

const moduleEscrowAddr = "aios1escrow000000000000000000000000000000"
const moduleFaucetAddr = "aios1faucet000000000000000000000000000000"

// moduleTreasuryAddr — destination for forfeit bonds. Phase 3.z step 3
// (treasury sweep) routes bonds that would otherwise accumulate in
// moduleEscrowAddr (early-deactivate service bonds; losing voucher bonds in
// the slash path) to this address. A future ADR routes treasury withdrawals
// to governance / burn / public-goods funding.
const ModuleTreasuryAddr = "aios1treasury00000000000000000000000000000"

// TreasuryBalance returns the chain's accumulated forfeit-bond balance.
// Read-only convenience for tests and the indexer.
func (s *State) TreasuryBalance() (uint64, error) {
	a, err := s.Account(ModuleTreasuryAddr)
	if err != nil {
		return 0, err
	}
	return a.Balance, nil
}

func (s *State) Account(addr string) (Account, error) {
	var acc Account
	acc.Address = addr
	err := s.db.View(func(tx *bbolt.Tx) error {
		bz := tx.Bucket(bktAccounts).Get([]byte(addr))
		if bz == nil {
			return nil
		}
		return json.Unmarshal(bz, &acc)
	})
	return acc, err
}

// Credit / debit are internal helpers. Callers are atomic-tx methods below.

func credit(tx *bbolt.Tx, addr string, amount uint64) error {
	if amount == 0 {
		return nil
	}
	acc := Account{Address: addr}
	if bz := tx.Bucket(bktAccounts).Get([]byte(addr)); bz != nil {
		_ = json.Unmarshal(bz, &acc)
	}
	acc.Balance += amount
	bz, _ := json.Marshal(acc)
	return tx.Bucket(bktAccounts).Put([]byte(addr), bz)
}

func debit(tx *bbolt.Tx, addr string, amount uint64) error {
	if amount == 0 {
		return nil
	}
	acc := Account{Address: addr}
	if bz := tx.Bucket(bktAccounts).Get([]byte(addr)); bz != nil {
		_ = json.Unmarshal(bz, &acc)
	}
	if acc.Balance < amount {
		return fmt.Errorf("%w: have %d need %d", types.ErrInsufficientFunds, acc.Balance, amount)
	}
	acc.Balance -= amount
	bz, _ := json.Marshal(acc)
	return tx.Bucket(bktAccounts).Put([]byte(addr), bz)
}

// ─── Nonces ─────────────────────────────────────────────────────────────────

func nextNonce(tx *bbolt.Tx, addr string) uint64 {
	return getUint64(tx.Bucket(bktNonces), []byte(addr))
}

func incNonce(tx *bbolt.Tx, addr string) error {
	cur := getUint64(tx.Bucket(bktNonces), []byte(addr))
	return putUint64(tx.Bucket(bktNonces), []byte(addr), cur+1)
}

// ─── Faucet (dev) ───────────────────────────────────────────────────────────

// EnsureDevKeyring creates alice + bob + faucet keys on first boot if absent,
// writes them to `path` (JSON), and credits initial balances.
func (s *State) EnsureDevKeyring(path string) error {
	if _, err := os.Stat(path); err == nil {
		// Already exists; load to learn the addresses but don't re-fund.
		return nil
	}
	type devKey struct {
		Name       string `json:"name"`
		Address    string `json:"address"`
		PubKeyHex  string `json:"pub_key_hex"`
		PrivKeyHex string `json:"priv_key_hex"`
	}
	type ring struct {
		ChainID string   `json:"chain_id"`
		Keys    []devKey `json:"keys"`
	}

	// Phase 3: include `harness` so the bundled determinism-harness has a
	// funded chain account from which to file MsgChallenge.
	// Phase 3.z step 3 / MVP item #2: include `harness-b` so two independent
	// watchers can voucher in parallel — required for any voucher-margin
	// setting > 0 and for credibly demonstrating sybil-resistant vouching.
	names := []string{"alice", "bob", "harness", "harness-b"}
	r := ring{ChainID: s.chainID}
	for _, name := range names {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return fmt.Errorf("generate %s: %w", name, err)
		}
		addr := types.AddressFromPubKey(pub)
		r.Keys = append(r.Keys, devKey{
			Name:       name,
			Address:    addr,
			PubKeyHex:  hex.EncodeToString(pub),
			PrivKeyHex: hex.EncodeToString(priv),
		})
	}

	bz, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(path, bz, 0o600); err != nil {
		return fmt.Errorf("write keyring: %w", err)
	}

	const initial = uint64(1_000_000_000)
	return s.db.Update(func(tx *bbolt.Tx) error {
		for _, k := range r.Keys {
			if err := credit(tx, k.Address, initial); err != nil {
				return err
			}
		}
		s.logger.Info("dev keyring created", zap.String("path", path),
			zap.String("alice", r.Keys[0].Address),
			zap.String("bob", r.Keys[1].Address),
			zap.String("harness", r.Keys[2].Address),
			zap.String("harness-b", r.Keys[3].Address))
		return nil
	})
}

// ─── Service & request stores ───────────────────────────────────────────────

func putService(tx *bbolt.Tx, svc types.Service) error {
	bz, _ := json.Marshal(svc)
	if err := tx.Bucket(bktServices).Put(uint64Key(svc.ID), bz); err != nil {
		return err
	}
	return tx.Bucket(bktSvcByName).Put([]byte(svc.Name), uint64Key(svc.ID))
}

func getService(tx *bbolt.Tx, id uint64) (types.Service, error) {
	bz := tx.Bucket(bktServices).Get(uint64Key(id))
	if bz == nil {
		return types.Service{}, types.ErrServiceNotFound
	}
	var svc types.Service
	if err := json.Unmarshal(bz, &svc); err != nil {
		return types.Service{}, err
	}
	return svc, nil
}

// voucherEligible reports whether `owner` operates at least one ACTIVE service
// in the given verification domain. Phase 3.z sybil-resistance gate for
// MsgVouch.
//
// "Active" is required (Phase 3.z step 2) so that registration-then-immediate-
// deactivation does not satisfy eligibility. Combined with the
// service-registration bond (`Params.ServiceRegistrationBond`) and the bond-
// forfeit-on-early-deactivation rule, a sybil voucher must:
//   1. Lock ServiceRegistrationBond per identity
//   2. Keep the service active through any disputes they vote on
//   3. Wait MinServiceLifetimeBlocks before they can reclaim the bond
// — turning a "free" sybil vote into a meaningful capital commitment.
func voucherEligible(tx *bbolt.Tx, owner string, domainID uint64) (bool, error) {
	if domainID == 0 {
		// Unverified-domain services have no membership concept; eligibility
		// is trivially satisfied (this preserves the Phase 0.5 demo path).
		return true, nil
	}
	c := tx.Bucket(bktServices).Cursor()
	for k, v := c.First(); k != nil; k, v = c.Next() {
		var svc types.Service
		if err := json.Unmarshal(v, &svc); err != nil {
			return false, err
		}
		if svc.Owner == owner && svc.VerificationDomainID == domainID && svc.Active {
			return true, nil
		}
	}
	return false, nil
}

// IsVoucherEligible exposes voucherEligible for read-only callers (tests, future
// queries). Returns true if `owner` operates a service in `domainID`.
func (s *State) IsVoucherEligible(owner string, domainID uint64) (bool, error) {
	var out bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		ok, err := voucherEligible(tx, owner, domainID)
		if err != nil {
			return err
		}
		out = ok
		return nil
	})
	return out, err
}

func putRequest(tx *bbolt.Tx, req types.InferenceRequest) error {
	bz, _ := json.Marshal(req)
	return tx.Bucket(bktRequests).Put(uint64Key(req.ID), bz)
}

func getRequest(tx *bbolt.Tx, id uint64) (types.InferenceRequest, error) {
	bz := tx.Bucket(bktRequests).Get(uint64Key(id))
	if bz == nil {
		return types.InferenceRequest{}, types.ErrRequestNotFound
	}
	var r types.InferenceRequest
	if err := json.Unmarshal(bz, &r); err != nil {
		return types.InferenceRequest{}, err
	}
	return r, nil
}

// ─── Domain store ───────────────────────────────────────────────────────────

func putDomain(tx *bbolt.Tx, d types.VerificationDomain) error {
	bz, _ := json.Marshal(d)
	return tx.Bucket(bktDomains).Put(uint64Key(d.ID), bz)
}

func getDomain(tx *bbolt.Tx, id uint64) (types.VerificationDomain, error) {
	bz := tx.Bucket(bktDomains).Get(uint64Key(id))
	if bz == nil {
		return types.VerificationDomain{}, types.ErrDomainNotFound
	}
	var d types.VerificationDomain
	if err := json.Unmarshal(bz, &d); err != nil {
		return types.VerificationDomain{}, err
	}
	return d, nil
}

func (s *State) GetDomain(id uint64) (types.VerificationDomain, error) {
	var out types.VerificationDomain
	err := s.db.View(func(tx *bbolt.Tx) error {
		v, e := getDomain(tx, id)
		out = v
		return e
	})
	return out, err
}

func (s *State) ListDomains() ([]types.VerificationDomain, error) {
	var out []types.VerificationDomain
	err := s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket(bktDomains).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var d types.VerificationDomain
			if err := json.Unmarshal(v, &d); err != nil {
				return err
			}
			out = append(out, d)
		}
		return nil
	})
	return out, err
}

// ─── Authority ──────────────────────────────────────────────────────────────

// SetAuthority installs the address allowed to call admin-only txs
// (e.g. MsgRegisterDomain). Idempotent. Phase 0.5 set this implicitly to bob
// via the first call from chain bootstrap. Phase 1+ governance replaces this.
func (s *State) SetAuthority(addr string) error {
	if !types.IsValidAddress(addr) {
		return types.ErrInvalidAddress
	}
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bktMeta).Put(keyAuthority, []byte(addr))
	})
}

func (s *State) Authority() string {
	var addr string
	_ = s.db.View(func(tx *bbolt.Tx) error {
		bz := tx.Bucket(bktMeta).Get(keyAuthority)
		addr = string(bz)
		return nil
	})
	return addr
}

// ─── Public reads ───────────────────────────────────────────────────────────

func (s *State) GetService(id uint64) (types.Service, error) {
	var out types.Service
	err := s.db.View(func(tx *bbolt.Tx) error {
		v, e := getService(tx, id)
		out = v
		return e
	})
	return out, err
}

func (s *State) ListServices() ([]types.Service, error) {
	var out []types.Service
	err := s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket(bktServices).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var svc types.Service
			if err := json.Unmarshal(v, &svc); err != nil {
				return err
			}
			out = append(out, svc)
		}
		return nil
	})
	return out, err
}

func (s *State) GetRequest(id uint64) (types.InferenceRequest, error) {
	var out types.InferenceRequest
	err := s.db.View(func(tx *bbolt.Tx) error {
		v, e := getRequest(tx, id)
		out = v
		return e
	})
	return out, err
}

func (s *State) ListRequests(filterStatus types.RequestStatus, filterService uint64) ([]types.InferenceRequest, error) {
	var out []types.InferenceRequest
	err := s.db.View(func(tx *bbolt.Tx) error {
		c := tx.Bucket(bktRequests).Cursor()
		for k, v := c.First(); k != nil; k, v = c.Next() {
			var r types.InferenceRequest
			if err := json.Unmarshal(v, &r); err != nil {
				return err
			}
			if filterStatus != "" && r.Status != filterStatus {
				continue
			}
			if filterService != 0 && r.ServiceID != filterService {
				continue
			}
			out = append(out, r)
		}
		return nil
	})
	return out, err
}

// AccountByName returns the address registered under a dev keyring name.
//
// Used by inference-node / demo script to find the provider account.
func (s *State) AccountByName(keyringPath, name string) (string, error) {
	bz, err := os.ReadFile(keyringPath)
	if err != nil {
		return "", err
	}
	var r struct {
		Keys []struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(bz, &r); err != nil {
		return "", err
	}
	for _, k := range r.Keys {
		if k.Name == name {
			return k.Address, nil
		}
	}
	return "", fmt.Errorf("key %q not found", name)
}

// ─── Event subscription (SSE) ───────────────────────────────────────────────

// Subscribe registers a channel for events. Returns an unsubscribe func.
// The channel is buffered; if a subscriber falls behind, events are dropped
// for that subscriber (logged at warn level).
func (s *State) Subscribe() (<-chan types.Event, func()) {
	ch := make(chan types.Event, 64)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	// Replay buffered events so a newly-attached subscriber sees recent history.
	replay := append([]types.Event(nil), s.eventLog...)
	s.mu.Unlock()

	// Send replay in a goroutine so Subscribe doesn't block.
	go func() {
		for _, e := range replay {
			select {
			case ch <- e:
			case <-time.After(2 * time.Second):
				return
			}
		}
	}()

	cancel := func() {
		s.mu.Lock()
		delete(s.subs, ch)
		close(ch)
		s.mu.Unlock()
	}
	return ch, cancel
}

// emit publishes an event to all subscribers and appends to the replay log.
func (s *State) emit(e types.Event) {
	s.mu.Lock()
	s.eventLog = append(s.eventLog, e)
	if len(s.eventLog) > eventLogLimit {
		s.eventLog = s.eventLog[len(s.eventLog)-eventLogLimit:]
	}
	subs := make([]chan types.Event, 0, len(s.subs))
	for ch := range s.subs {
		subs = append(subs, ch)
	}
	s.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- e:
		default:
			s.logger.Warn("subscriber dropped event (slow consumer)", zap.String("type", string(e.Type)))
		}
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func uint64Key(n uint64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(n)
		n >>= 8
	}
	return b
}

func putUint64(b *bbolt.Bucket, key []byte, v uint64) error {
	return b.Put(key, uint64Key(v))
}

func getUint64(b *bbolt.Bucket, key []byte) uint64 {
	bz := b.Get(key)
	if len(bz) != 8 {
		return 0
	}
	var n uint64
	for i := 0; i < 8; i++ {
		n = (n << 8) | uint64(bz[i])
	}
	return n
}

// silence unused-import linter if errors aren't referenced above.
var _ = errors.New
