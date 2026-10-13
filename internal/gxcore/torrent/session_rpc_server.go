package torrent

import (
	"context"
	"expvar"
	"net"
	"net/http"
	"net/rpc"
	"strconv"
	"time"

	"github.com/buzzqw/gextto/internal/gxcore/internal/logger"
	"github.com/powerman/rpc-codec/jsonrpc2"
)

// rpcServer exposes the session over JSON-RPC (for the gx-torrent daemon)
// plus the internal /move-torrent transfer endpoint and expvar debug vars.
type rpcServer struct {
	rpcServer  *rpc.Server
	httpServer http.Server
	log        logger.Logger
}

func newRPCServer(ses *Session) *rpcServer {
	h := &rpcHandler{session: ses}
	srv := rpc.NewServer()
	_ = srv.RegisterName("Session", h)

	return &rpcServer{
		rpcServer: srv,
		httpServer: http.Server{
			Handler: newRPCMux(srv, h),
		},
		log: logger.New("rpc server"),
	}
}

// newRPCMux routes the three endpoints served on the RPC listener. Both the
// JSON-RPC API and the move-torrent transfer need the same session handler.
func newRPCMux(srv *rpc.Server, h *rpcHandler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/debug/vars", expvar.Handler())
	mux.HandleFunc("/move-torrent", h.handleMoveTorrent)
	mux.Handle("/", jsonrpc2.HTTPHandler(srv))
	return mux
}

func (s *rpcServer) Start(host string, port int) error {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.log.Infoln("RPC server is listening on", listener.Addr().String())

	go s.serve(listener)
	return nil
}

// serve runs until the listener closes. Serve errors are logged, never
// fatal: the torrent package is embeddable as a library, so an unexpected
// error must not os.Exit the host process.
func (s *rpcServer) serve(listener net.Listener) {
	if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
		s.log.Errorln("rpc server stopped serving:", err)
	}
}

func (s *rpcServer) Stop(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return s.httpServer.Shutdown(ctx)
}
