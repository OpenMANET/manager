/*
Copyright © 2026 OpenMANET

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program. If not, see <http://www.gnu.org/licenses/>.
*/
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/openmanet/openmanetd/internal/config"
	"github.com/openmanet/openmanetd/internal/frontend"
	"github.com/openmanet/openmanetd/internal/util/logger"
	"github.com/spf13/cobra"
)

var frontendCmd = &cobra.Command{ //nolint:gochecknoglobals
	Use:   "frontend",
	Short: "Run only the frontend server for development",
	Long: `Starts only the frontend HTTP/WebSocket server and connects it to a
remote openmanetd instance. This is useful for frontend development without
running the full application stack (database, Alfred, GPS, comms, BLOS, etc.).

The frontend server serves the embedded React SPA and proxies all API and
WebSocket requests to the remote openmanetd API specified by --api-address.

Examples:
  openmanetd frontend --api-address http://10.41.1.1:8087
  openmanetd frontend --api-address http://10.41.1.1:8087 --port 3000`,
	Run: runFrontend,
}

func init() {
	rootCmd.AddCommand(frontendCmd)

	frontendCmd.Flags().String("api-address", "", "URL of the remote openmanetd API (e.g. http://10.41.1.1:8087)")
	frontendCmd.Flags().Int("port", 8081, "local port for the frontend server")

	_ = frontendCmd.MarkFlagRequired("api-address")
}

func runFrontend(cmd *cobra.Command, _ []string) {
	apiAddress, _ := cmd.Flags().GetString("api-address")
	port, _ := cmd.Flags().GetInt("port")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	log := logger.InitLogging(ctx)

	cfg := config.New(nil)
	cfg.SetOpenMANETAPIAddress(apiAddress)
	cfg.SetOpenMANETWebsocketPort(port)

	log.Info().
		Str("api-address", apiAddress).
		Int("port", port).
		Msg("starting frontend-only dev server")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	srv := frontend.NewFrontendServer(ctx, cfg, staticFS)

	errCh := make(chan error, 1)

	go func() {
		errCh <- srv.Run(ctx)
	}()

	select {
	case err := <-errCh:
		log.Fatal().Err(err).Msg("frontend server failed")
	case <-sig:
		fmt.Println()
		log.Info().Msg("shutting down frontend-only dev server")
		cancel()
	}
}
