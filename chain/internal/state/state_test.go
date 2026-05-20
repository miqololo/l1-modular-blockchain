package state_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/aios/aios/internal/state"
	"github.com/aios/aios/internal/types"
)

func newState(t *testing.T) *state.State {
	t.Helper()
	dir := t.TempDir()
	s, err := state.Open(filepath.Join(dir, "test.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	// Tests that don't specifically exercise the Phase 3.z step 2 service-
	// registration bond should not have to fund every signer. Zero out the
	// bond by default; bond-specific tests use newStateWithBond.
	p := types.DefaultParams()
	p.ServiceRegistrationBond = types.Coin{Denom: "aios"}
	p.MinServiceLifetimeBlocks = 0
	require.NoError(t, s.SetParams(p))
	return s
}

// newStateWithBond returns a state configured with non-zero service-registration
// bond + a positive minimum lifetime. Used by the Phase 3.z step 2 tests that
// assert bond lock / refund / forfeit semantics.
func newStateWithBond(t *testing.T, bondAmount uint64, lifetime int64) *state.State {
	t.Helper()
	dir := t.TempDir()
	s, err := state.Open(filepath.Join(dir, "test.db"), zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	p := types.DefaultParams()
	p.ServiceRegistrationBond = types.Coin{Denom: "aios", Amount: bondAmount}
	p.MinServiceLifetimeBlocks = lifetime
	require.NoError(t, s.SetParams(p))
	return s
}

// makeKey returns (pub, priv, addr) for a fresh Ed25519 key.
func makeKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return pub, priv, types.AddressFromPubKey(pub)
}

// signTx wraps a payload in a SignedTx with a real signature.
func signTx(t *testing.T, priv ed25519.PrivateKey, pub ed25519.PublicKey, nonce uint64, txType types.TxType, payload any) types.SignedTx {
	t.Helper()
	bz, err := json.Marshal(payload)
	require.NoError(t, err)
	tx := types.SignedTx{
		Type:      txType,
		Nonce:     nonce,
		PubKeyHex: hex.EncodeToString(pub),
		Payload:   bz,
	}
	canonical, err := tx.CanonicalBytes()
	require.NoError(t, err)
	tx.SignatureHex = hex.EncodeToString(ed25519.Sign(priv, canonical))
	return tx
}

// fund credits an account directly via a faucet-style internal tx.
// In production this is done via genesis or governance; tests use a direct
// state mutation through the public Transfer flow.
func fund(t *testing.T, s *state.State, priv ed25519.PrivateKey, pub ed25519.PublicKey, dest string, amount uint64) {
	t.Helper()
	// Bootstrap: we can't actually fund from scratch via tx; tests use the
	// state-level helper by calling EnsureDevKeyring on the funded path. For
	// this test setup we go even simpler: directly credit via state internals
	// is unavailable, so we use a helper that mints via the meta admin path.
	// Instead, every test here that needs funds uses a precondition: register
	// a service first (free), or uses the dev keyring.
	_ = priv
	_ = pub
	_ = dest
	_ = amount
}

func TestRegisterService_HappyPath(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)

	tx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "translate-en-fr", Description: "demo",
		Price: types.Coin{Denom: "aios", Amount: 100},
	})
	receipt, err := s.SubmitTx(tx)
	require.NoError(t, err)
	require.Equal(t, uint64(1), receipt.NewID)

	svc, err := s.GetService(1)
	require.NoError(t, err)
	require.Equal(t, "translate-en-fr", svc.Name)
	require.Equal(t, addr, svc.Owner)
	require.True(t, svc.Active)
}

func TestRegisterService_RejectsZeroPrice(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)

	tx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "x", Price: types.Coin{Denom: "aios", Amount: 0},
	})
	_, err := s.SubmitTx(tx)
	require.ErrorIs(t, err, types.ErrZeroPrice)
}

func TestRegisterService_RejectsDuplicateName(t *testing.T) {
	s := newState(t)
	pub1, priv1, addr1 := makeKey(t)
	pub2, priv2, addr2 := makeKey(t)

	tx1 := signTx(t, priv1, pub1, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr1, Name: "translate", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(tx1)
	require.NoError(t, err)

	tx2 := signTx(t, priv2, pub2, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr2, Name: "translate", Price: types.Coin{Denom: "aios", Amount: 50},
	})
	_, err = s.SubmitTx(tx2)
	require.ErrorIs(t, err, types.ErrDuplicateName)
}

func TestSignature_RejectsWrongKey(t *testing.T) {
	s := newState(t)
	pub, _, addr := makeKey(t)
	_, priv2, _ := makeKey(t)

	// Sign with priv2 but claim pub belongs to addr.
	tx := signTx(t, priv2, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "x", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(tx)
	require.ErrorIs(t, err, types.ErrInvalidSignature)
}

func TestNonce_RejectsReplay(t *testing.T) {
	s := newState(t)
	pub, priv, addr := makeKey(t)

	tx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "x", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(tx)
	require.NoError(t, err)

	// Replay the same tx → nonce check fails.
	_, err = s.SubmitTx(tx)
	require.ErrorIs(t, err, types.ErrInvalidNonce)
}

func TestEvents_EmittedOnRegister(t *testing.T) {
	s := newState(t)
	ch, cancel := s.Subscribe()
	defer cancel()

	pub, priv, addr := makeKey(t)
	tx := signTx(t, priv, pub, 0, types.TxRegisterService, types.MsgRegisterService{
		Owner: addr, Name: "translate", Price: types.Coin{Denom: "aios", Amount: 100},
	})
	_, err := s.SubmitTx(tx)
	require.NoError(t, err)

	// Drain channel for up to one event matching our type.
	var found bool
	for i := 0; i < 5 && !found; i++ {
		select {
		case ev := <-ch:
			if ev.Type == types.EventServiceRegistered {
				found = true
			}
		default:
		}
	}
	require.True(t, found, "expected EventServiceRegistered")
}

func TestAddressDerivation(t *testing.T) {
	pub, _, addr := makeKey(t)
	require.True(t, types.IsValidAddress(addr))
	require.Equal(t, addr, types.AddressFromPubKey(pub))
	require.False(t, types.IsValidAddress("aios1zzz"))
	require.False(t, types.IsValidAddress("cosmos1abc"))
}
