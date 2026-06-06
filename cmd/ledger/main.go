// Command ledger is the CWB issue-tracker gRPC service. It runs behind
// interchange-gateway over mTLS; identity comes from cwb-* gRPC metadata
// injected by the gateway.
//
// Config (env):
//
//	LEDGER_GRPC_ADDR   listen address (default :8081)
//	LEDGER_DB          sqlite path (default /var/lib/cwb/ledger.db)
//	LEDGER_TLS_CERT    path to server TLS certificate (PEM)
//	LEDGER_TLS_KEY     path to server TLS private key (PEM)
//	LEDGER_TLS_CA      path to client CA certificate (PEM) for mTLS
//	LEDGER_DEV_INSECURE set to "1" to skip mTLS (local dev only; fatal if unset without certs)
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net"
	"os"

	cwbv1 "github.com/CarriedWorldUniverse/cwb-proto/gen/go/cwb/v1"
	"github.com/CarriedWorldUniverse/ledger"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

func main() {
	addr := env("LEDGER_GRPC_ADDR", ":8081")
	dbPath := env("LEDGER_DB", "/var/lib/cwb/ledger.db")

	svc, err := ledger.New(context.Background(), ledger.Config{
		DBPath: dbPath,
	})
	if err != nil {
		log.Fatalf("ledger: open %q: %v", dbPath, err)
	}
	defer svc.Close()

	grpcSrv := grpc.NewServer(serverOptions()...)
	cwbv1.RegisterIssueServiceServer(grpcSrv, ledger.NewIssueServer(svc))
	cwbv1.RegisterProjectServiceServer(grpcSrv, ledger.NewProjectServer(svc))
	cwbv1.RegisterOrgServiceServer(grpcSrv, ledger.NewOrgServer(svc))
	cwbv1.RegisterAdminServiceServer(grpcSrv, ledger.NewAdminServer(svc))

	healthSrv := health.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcSrv, healthSrv)
	healthSrv.SetServingStatus("cwb.v1.IssueService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthSrv.SetServingStatus("cwb.v1.ProjectService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthSrv.SetServingStatus("cwb.v1.OrgService", grpc_health_v1.HealthCheckResponse_SERVING)
	healthSrv.SetServingStatus("cwb.v1.AdminService", grpc_health_v1.HealthCheckResponse_SERVING)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("ledger: listen %s: %v", addr, err)
	}
	log.Printf("ledger gRPC listening on %s (db=%s)", addr, dbPath)
	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatalf("ledger: serve: %v", err)
	}
}

// serverOptions builds the gRPC server options. When the TLS env vars are
// set the server enforces mTLS (RequireAndVerifyClientCert). Insecure mode
// requires an explicit LEDGER_DEV_INSECURE=1 opt-in; missing certs without
// the opt-in cause a fatal startup error.
func serverOptions() []grpc.ServerOption {
	certFile := os.Getenv("LEDGER_TLS_CERT")
	keyFile := os.Getenv("LEDGER_TLS_KEY")
	caFile := os.Getenv("LEDGER_TLS_CA")
	if certFile == "" || keyFile == "" || caFile == "" {
		if os.Getenv("LEDGER_DEV_INSECURE") == "1" {
			log.Printf("ledger: LEDGER_DEV_INSECURE=1 — starting WITHOUT mTLS (dev only)")
			return nil
		}
		log.Fatalf("ledger: mTLS required — set LEDGER_TLS_CERT/_KEY/_CA (or LEDGER_DEV_INSECURE=1 for local dev)")
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatalf("ledger: tls: load cert/key: %v", err)
	}
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		log.Fatalf("ledger: tls: read CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		log.Fatalf("ledger: tls: no certs parsed from CA file %s", caFile)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		MinVersion:   tls.VersionTLS13,
	}
	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(tlsCfg))}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
