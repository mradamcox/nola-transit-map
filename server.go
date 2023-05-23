package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"time"

	"nhooyr.io/websocket"
)

type Server struct {
	Config     ServerConfig
	Subscriber interface {
		Subscribe(context.Context, *websocket.Conn) error
	}
	Log interface {
		Printf(fomat string, v ...any)
	}
	Mux *http.ServeMux
}

type ServerConfig struct {
	Addr    string
	Timeout time.Duration
}

// Start attaches mux handlers and maintains the long-running server.
func (s Server) Start(ctx context.Context) error {
	s.Mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./public/index.html")
	})
	s.Mux.Handle("/public/",
		http.StripPrefix("/public/", http.FileServer(http.Dir("./public"))))

	s.Mux.HandleFunc("/ws", s.newWebSocketHandler())

	s.Log.Printf("INFO: starting server")
	server := &http.Server{
		Addr:         s.Config.Addr,
		Handler:      s.Mux,
		ReadTimeout:  s.Config.Timeout,
		WriteTimeout: s.Config.Timeout,
		ErrorLog:     s.Log.(*log.Logger),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	errc := make(chan error, 1)
	defer close(errc)
	go func() {
		errc <- server.ListenAndServe()
	}()
	select {
	case err := <-errc:
		s.Log.Printf("ERROR: server failed: %s", err)
	case <-ctx.Done():
	}
	s.Log.Printf("INFO: shutting server down")
	// shutdown gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return server.Shutdown(ctx)
}

// newWebSocketHandler upgrades a request to a long-running websocket connection.
func (s Server) newWebSocketHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			s.Log.Printf("websocket upgrade failed: %s", err)
			return
		}
		defer conn.Close(websocket.StatusInternalError, "")

		err = s.Subscriber.Subscribe(r.Context(), conn)
		if errors.Is(err, context.Canceled) ||
			websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
			websocket.CloseStatus(err) == websocket.StatusGoingAway {
			s.Log.Printf("INFO: websocket subscriber disconnected: %s", err)
			return
		}
		if err != nil {
			s.Log.Printf("ERROR: websocket subscriber failed: %s", err)
			return
		}
	}
}
