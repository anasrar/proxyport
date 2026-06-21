package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

type Proxy struct {
	Domain       string                 `json:"domain"`
	Port         int                    `json:"port"`
	ReverseProxy *httputil.ReverseProxy `json:"-"`
}

var (
	proxies = make([]Proxy, 0)
	mapping = make(map[string]Proxy, 0)
)

var Version = "dev"

func main() {
	cmd := &cobra.Command{
		Use:     "proxyport",
		Version: Version,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			for _, m := range proxies {
				if m.Domain == "" {
					return fmt.Errorf("port %d missing domain", m.Port)
				}

				if m.Port == 0 {
					return fmt.Errorf("domain %s missing port", m.Domain)
				}

				target, err := url.Parse(fmt.Sprintf("http://localhost:%d", m.Port))
				if err != nil {
					return err
				}

				m.ReverseProxy = &httputil.ReverseProxy{
					Rewrite: func(pr *httputil.ProxyRequest) {
						pr.SetURL(target)

						pr.Out.Host = pr.In.Host
					},
				}
				mapping[fmt.Sprintf("%s.localhost", m.Domain)] = m
			}

			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			ctxShutdown, cancelShutdown := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancelShutdown()

			server := &http.Server{
				Addr: "127.0.0.1:80",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					proxy, ok := mapping[r.Host]
					str := fmt.Sprintf("%s %s %s", r.Method, r.Host, r.URL)

					if ok {
						proxy.ReverseProxy.ServeHTTP(w, r)
						str = fmt.Sprintf("%s -> %d", str, proxy.Port)
					} else {
						w.WriteHeader(http.StatusBadGateway)
						w.Write([]byte(fmt.Sprintf("Proxy for domain %s not yet assigned", r.Host)))
					}

					log.Print(str)
				}),
			}

			go func() {
				log.Print("proxyport running...")
				if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
					log.Fatal(err)
				}
			}()

			<-ctxShutdown.Done()
			log.Print("proxyport shutdown...")

			ctxServerShutdown, cancelServerShutdown := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelServerShutdown()

			if err := server.Shutdown(ctxServerShutdown); err != nil {
				log.Fatal(err)
			}
		},
	}

	cmd.Flags().FuncP("domain", "d", "domain", func(val string) error {
		if len(proxies) > 0 && proxies[len(proxies)-1].Port == 0 {
			return fmt.Errorf("domain missing port")
		}

		if val == "" {
			return fmt.Errorf("domain cannot be empty")
		}

		proxies = append(proxies, Proxy{
			Domain: val,
		})

		return nil
	})

	cmd.Flags().FuncP("port", "p", "port", func(val string) error {
		if len(proxies) == 0 {
			return fmt.Errorf("port requires domain")
		}

		if proxies[len(proxies)-1].Port != 0 {
			return fmt.Errorf("port already set")
		}

		p, err := strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("invalid port: %w", err)
		}

		proxies[len(proxies)-1].Port = p

		return nil
	})

	_ = cmd.Execute()
}
