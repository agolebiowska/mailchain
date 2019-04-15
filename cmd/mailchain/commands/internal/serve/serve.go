// Copyright 2019 Finobo
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package serve

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/mailchain/mailchain/cmd/mailchain/commands/internal/prerun"
	"github.com/mailchain/mailchain/cmd/mailchain/config"
	"github.com/mailchain/mailchain/cmd/mailchain/config/defaults"
	"github.com/mailchain/mailchain/internal/pkg/http/rest/addresses"
	"github.com/mailchain/mailchain/internal/pkg/http/rest/ethereum/address/messages"
	"github.com/mailchain/mailchain/internal/pkg/http/rest/ethereum/address/publickey"
	"github.com/mailchain/mailchain/internal/pkg/http/rest/ethereum/messages/send"
	"github.com/mailchain/mailchain/internal/pkg/http/rest/messages/read"
	"github.com/mailchain/mailchain/internal/pkg/http/rest/spec"
	"github.com/mailchain/mailchain/internal/pkg/keystore/kdf/multi"
	"github.com/mailchain/mailchain/internal/pkg/keystore/kdf/scrypt"
	"github.com/pkg/errors"
	"github.com/rs/cors"
	log "github.com/sirupsen/logrus" // nolint:depguard
	"github.com/spf13/cobra"
	"github.com/spf13/viper" // nolint:depguard
	"github.com/ttacon/chalk"
	"github.com/urfave/negroni"
)

func Cmd() (*cobra.Command, error) {
	cmd := &cobra.Command{
		Use:               "serve",
		Short:             "Serve the mailchain application",
		PersistentPreRunE: prerun.InitConfig,
		RunE: func(cmd *cobra.Command, args []string) error {
			router := mux.NewRouter()
			router.HandleFunc("/api/spec.json", spec.Get()).Methods("GET")
			router.HandleFunc("/api/docs", spec.DocsGet()).Methods("GET")

			receivers, err := config.Receivers()
			if err != nil {
				return errors.WithMessage(err, "Could not configure receivers")
			}
			pubKeyFinders, err := config.PublicKeyFinders()
			if err != nil {
				return errors.WithMessage(err, "Could not configure receivers")
			}
			senders, err := config.Senders()
			if err != nil {
				return errors.WithMessage(err, "Could not configure senders")
			}

			senderStorage, err := config.SenderStorage()
			if err != nil {
				return errors.WithMessage(err, "Could not config store")
			}
			mailboxStore, err := config.InboxStore()
			if err != nil {
				return errors.WithMessage(err, "Could not config mailbox store")
			}
			keystore, err := config.KeyStore()
			if err != nil {
				return errors.WithMessage(err, "could not create `keystore`")
			}
			passphrase, err := config.Passphrase(cmd)
			if err != nil {
				return errors.WithMessage(err, "could not get `passphrase`")
			}
			// TODO: currently this only does scrypt need flag + config etc
			deriveKeyOptions := multi.OptionsBuilders{
				Scrypt: []scrypt.DeriveOptionsBuilder{scrypt.WithPassphrase(passphrase)},
			}
			router.HandleFunc("/api/addresses", addresses.Get(keystore)).Methods("GET")
			router.HandleFunc("/api/ethereum/{network}/address/{address:[-0-9a-zA-Z]+}/public-key", publickey.Get(pubKeyFinders)).Methods("GET")
			router.HandleFunc(
				"/api/ethereum/{network}/address/{address:[-0-9a-zA-Z]+}/messages",
				messages.Get(mailboxStore, receivers, keystore, deriveKeyOptions)).Methods("GET")
			router.HandleFunc("/api/ethereum/{network}/messages/send", send.Post(senderStorage, senders, keystore, deriveKeyOptions)).Methods("POST")
			router.HandleFunc("/api/messages/{message_id}/read", read.Get(mailboxStore)).Methods("GET")
			router.HandleFunc("/api/messages/{message_id}/read", read.Put(mailboxStore)).Methods("PUT")
			router.HandleFunc("/api/messages/{message_id}/read", read.Delete(mailboxStore)).Methods("DELETE")

			_ = router.Walk(gorillaWalkFn)

			fmt.Println(chalk.Bold.TextStyle(fmt.Sprintf(
				"Find out more by visiting the docs http://127.0.0.1:%d/api/docs",
				viper.GetInt("server.port"))))

			createNegroni(router).Run(fmt.Sprintf(":%d", viper.GetInt("server.port")))
			return nil
		},
	}

	if err := setupFlags(cmd); err != nil {
		return nil, err
	}
	return cmd, nil
}
func setupFlags(cmd *cobra.Command) error {
	cmd.Flags().Int("port", defaults.Port, "Port to run server on")
	cmd.Flags().Bool("cors-disabled", defaults.CORSDisabled, "Disable CORS on the server")
	cmd.Flags().String("cors-allowed-origins", defaults.CORSAllowedOrigins, "Allowed origins for CORS")

	if err := viper.BindPFlag("server.port", cmd.Flags().Lookup("port")); err != nil {
		return err
	}
	if err := viper.BindPFlag("server.cors.disabled", cmd.Flags().Lookup("cors-disabled")); err != nil {
		return err
	}
	if err := viper.BindPFlag("server.cors.allowed-origins", cmd.Flags().Lookup("cors-allowed-origins")); err != nil {
		return err
	}

	cmd.PersistentFlags().String("passphrase", "", "Passphrase to encrypt/decrypt key with")
	return nil
}

func createNegroni(router http.Handler) *negroni.Negroni {
	n := negroni.New()
	if !viper.GetBool("server.cors.disabled") {
		n.Use(cors.New(cors.Options{
			AllowedOrigins: []string{viper.GetString("server.cors.allowed-origins")},
			AllowedHeaders: []string{"Authorization", "Content-Type"},
			AllowedMethods: []string{"GET", "PUT", "DELETE", "POST", "HEAD", "PATCH"},
			MaxAge:         86400,
		}))
	}
	n.UseHandler(router)
	return n
}

func gorillaWalkFn(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
	methods, _ := route.GetMethods()
	for _, method := range methods {
		path, _ := route.GetPathTemplate()
		log.Infof("Serving %s : %s", method, path)
	}
	return nil
}
