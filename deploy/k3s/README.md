# ledger — k3s deploy (cwb namespace)

ledger is a CWB product: a herald-identified HTTP issue-tracker reached
only through the interchange-gateway over an mTLS hop. It joins the `cwb`
namespace defined by herald's `00-namespace.yaml` (not redefined here).

## Build + load the image

    podman build -f cmd/ledger/Containerfile -t ledger:dev .
    podman save ledger:dev | sudo k3s ctr images import -

## Apply

    kubectl apply -f deploy/k3s/10-pvc.yaml
    kubectl apply -f deploy/k3s/20-deployment.yaml
    kubectl apply -f deploy/k3s/30-service.yaml

Readiness/liveness probe `/healthz/issues` is public (tokenless) in every
auth mode, so kubelet can reach it directly.

## Gateway route

Add to the interchange-gateway's route config so `/ledger/*` proxies to
this service (the gateway strips the `/ledger` prefix, so ledger sees
clean `/api/...` paths):

    /ledger -> http://ledger.cwb.svc.cluster.local:8081

The gateway verifies the herald token and injects `X-CWB-{Subject,Org,
Kind,Scopes}`; ledger runs in `LEDGER_AUTH_MODE=gateway` and trusts those
headers because the gateway->ledger hop is mTLS and ledger has no ingress
of its own.

## mTLS on the gateway<->ledger hop

Platform-level decision (`project_cwb_tls_everywhere`): the service mesh
(Linkerd) or cert-manager internal certs secure the hop. Whichever the
platform pins is applied here at deploy time (mesh sidecar injection via
namespace/pod annotation, or a cert volume) — shared across all CWB
pillars, not redefined per service. No plain-HTTP hop in the path: public
TLS at Cloudflare, Full-strict to the origin gateway, mTLS gateway<->ledger.

For the interim dMon dev deploy, the intra-cluster gateway<->ledger hop is
plain HTTP until the mesh-mTLS platform step lands; this is dev-only.
