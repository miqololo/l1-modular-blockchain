// Dev convenience endpoint: returns the chain's dev keyring (alice, bob) so the
// in-browser wallet can "import" a funded account for the demo.
//
// SCOPED TO DEMO: this endpoint exposes private keys. It is gated by an env var
// and only runs when `EXPOSE_DEV_KEYRING=1`. In production / mainnet wiring this
// route is removed.
import fs from "node:fs";

export const dynamic = "force-dynamic";

const KEYRING_PATH = process.env.AIOS_KEYRING_PATH || "/keys/keys.json";

export async function GET() {
  if (process.env.EXPOSE_DEV_KEYRING !== "1") {
    return Response.json({ error: "dev keyring not exposed" }, { status: 404 });
  }
  try {
    const bz = fs.readFileSync(KEYRING_PATH, "utf8");
    return new Response(bz, {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  } catch (err) {
    return Response.json({ error: (err as Error).message }, { status: 500 });
  }
}
