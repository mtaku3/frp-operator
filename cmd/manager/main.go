// Command manager is the frp-operator binary entrypoint. It parses
// configuration from CLI flags + environment variables and hands control
// to pkg/operator.Run.
package main

import (
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/mtaku3/frp-operator/pkg/operator"
)

func main() {
	cfg, err := operator.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}

	zapLog := zap.New(zap.UseDevMode(false))
	log.SetLogger(zapLog)
	ctrl.SetLogger(zapLog)

	ctx := ctrl.LoggerInto(ctrl.SetupSignalHandler(), zapLog)

	if err := operator.Run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "operator: %v\n", err)
		os.Exit(1)
	}
}
