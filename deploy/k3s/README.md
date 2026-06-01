# ledger — k3s deploy (cwb namespace)

ledger is a CWB product: a herald-identified **gRPC** issue-tracker reached
only through the interchange-gateway over an mTLS hop. It joins the `cwb`
namespace defined by herald's `00-namespace.yaml` (not redefined here).

## Prerequisites

- **cert-manager** is installed in the cluster.
- The shared internal CA (`cwb-ca` Issuer) exists. Apply
  `commonplace/deploy/k3s/05-certs.yaml` first if it hasn't been deployed yet.
- `05-cert.yaml` (this repo) provisions the `ledger-tls` server cert used for
  gRPC+mTLS on `:8081`.

## Build + load the image

    podman build -f cmd/ledger/Containerfile -t ledger:dev .
    podman save ledger:dev | sudo k3s ctr images import -

## Apply

    kubectl apply -f deploy/k3s/05-cert.yaml
    kubectl -n cwb wait --for=condition=Ready certificate/ledger-tls --timeout=120s
    kubectl apply -f deploy/k3s/10-pvc.yaml
    kubectl apply -f deploy/k3s/20-deployment.yaml
    kubectl apply -f deploy/k3s/30-service.yaml

Or apply the whole directory in one shot (cert-manager resolves ordering via
the secret readiness gate on pod startup):

    kubectl apply -f deploy/k3s/

## gRPC + mTLS

ledger's server listens on `LEDGER_GRPC_ADDR` (`:8081`) with mTLS enforced.
Callers must present a client cert signed by `cwb-ca`. Interchange and cairn
each mount their own `*-client-tls` cert for the outbound connection.

The readiness/liveness probes use `tcpSocket` on `:8081` — the gRPC server
has no HTTP health endpoint.

## Local / dev without mTLS

Set `LEDGER_DEV_INSECURE=1` to start the gRPC server without TLS (plain gRPC,
no cert required). Useful for unit tests and local smoke runs where cert-manager
is not available.

## Gateway route

`/ledger` is **not** in `INTERCHANGE_ROUTES` — ledger is no longer an HTTP
reverse-proxy backend. Interchange talks to ledger directly over gRPC via
`INTERCHANGE_LEDGER_GRPC=ledger.cwb.svc:8081`.
