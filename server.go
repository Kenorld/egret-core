package egret

import (
	"crypto/tls"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"

	enet "github.com/kenorld/egret-core/net"
	"golang.org/x/crypto/acme/autocert"
	"golang.org/x/net/websocket"
)

// This method handles all requests.  It dispatches to handleInternal after
// handling / adapting websocket connections.
func handle(w http.ResponseWriter, r *http.Request) {
	if maxRequestSize := int64(Config.GetIntDefault("http.max_request_size", 0)); maxRequestSize > 0 {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestSize)
	}

	upgrade := r.Header.Get("Upgrade")
	if upgrade == "websocket" || upgrade == "Websocket" {
		websocket.Handler(func(ws *websocket.Conn) {
			//Override default Read/Write timeout with sane value for a web socket request
			ws.SetDeadline(time.Now().Add(time.Hour * 24))
			r.Method = "WS"
			handleInternal(w, r, ws)
		}).ServeHTTP(w, r)
	} else {
		handleInternal(w, r, nil)
	}
}

func handleInternal(w http.ResponseWriter, r *http.Request, ws *websocket.Conn) {
	start := time.Now()
	var (
		req  = NewRequest(r)
		resp = NewResponse(w)
		c    = NewContext(req, resp)
	)
	req.Websocket = ws
	for _, router := range routers {
		c.Handlers, c.Params = router.Match(req.Method, req.URL)
		if len(c.Handlers) > 0 {
			break
		}
	}
	if len(c.Handlers) == 0 {
		c.NotFound("No handle found!")
	}
	c.Next()

	err := c.ExecuteRender()
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"Error": err,
		}).Info("Render error.")
	}
	// Close the Writer if we can
	if w, ok := resp.Writer.(io.Closer); ok {
		w.Close()
	}

	if DevMode {
		// Egret request access log format
		// RequestStartTime ClientIP ResponseStatus RequestLatency HTTPMethod URLPath
		// Sample format:
		// 2016/05/25 17:46:37.112 127.0.0.1 200  270.157µs GET /
		logrus.WithFields(logrus.Fields{
			"ClientIP": ClientIP(r),
			"Status":   c.Response.Status,
			"Time":     time.Since(start),
			"Method":   req.Method,
			"Path":     req.URL.Path,
		}).Info("Client requested")
	}
}

// Serve the server.
// This is called from the generated main file.
// If port is non-zero, use that.  Else, read the port from app.yaml.
func Serve(port int) *Server {
	address := HttpAddr
	if port == 0 {
		port = HttpPort
	}

	var network = HttpNetwork
	var localAddress string

	// If the port is zero, treat the address as a fully qualified local address.
	// This address must be prefixed with the network type followed by a colon,
	// e.g. unix:/tmp/app.socket or tcp6:::1 (equivalent to tcp6:0:0:0:0:0:0:0:1)
	if port == 0 {
		parts := strings.SplitN(address, ":", 2)
		network = parts[0]
		localAddress = parts[1]
	} else {
		localAddress = address + ":" + strconv.Itoa(port)
	}

	server := &Server{
		network:        network,
		tlsEnabled:     HttpTLSEnabled,
		tlsCertFile:    HttpTLSCert,
		tlsKeyFile:     HttpTLSKey,
		letsencrypt:    HttpTLSLetsEncrypt,
		letsencryptDir: HttpTLSLetsEncryptDir,
		unixFileMode:   UnixFileMode,
		Server: &http.Server{
			Addr:         localAddress,
			Handler:      http.HandlerFunc(handle),
			ReadTimeout:  time.Duration(Config.GetIntDefault("timeout.read", 0)) * time.Second,
			WriteTimeout: time.Duration(Config.GetIntDefault("timeout.write", 0)) * time.Second,
		},
	}

	server.run()
	return server
}

// Server web server object
type Server struct {
	nameWithVersion string
	network         string
	tlsEnabled      bool
	tlsCertFile     string
	tlsKeyFile      string
	letsencrypt     bool
	letsencryptDir  string
	unixFileMode    os.FileMode
	*http.Server
}

func (server *Server) run() {
	server.initAddr()
	ln := server.listen()

	typ := strings.ToUpper(server.network)
	if server.tlsEnabled {
		typ += "/HTTP2"
	}
	Logger.Printf("Egret listen and serve %s on %v", typ, server.Addr)

	err := server.Server.Serve(ln)
	if realServeError(err) != nil {
		Logger.Fatalln("%v\n", err)
	}
}

func (server *Server) initAddr() {
	if server.tlsEnabled {
		if server.Addr == "" {
			server.Addr = ":http"
		}
	} else {
		if server.Addr == "" {
			server.Addr = ":https"
		}
	}
}

var graceNet = new(enet.Net)

func (server *Server) listen() net.Listener {
	if server.tlsEnabled {
		if HttpTLSCert != "" && HttpTLSKey != "" {
			var cert tls.Certificate
			cert, err := tls.LoadX509KeyPair(HttpTLSCert, HttpTLSKey)
			if err != nil {
				log.Fatalln("%v\n", err)
				return nil
			}
			server.TLSConfig = &tls.Config{
				Certificates:             []tls.Certificate{cert},
				NextProtos:               []string{"http/1.1", "h2"},
				PreferServerCipherSuites: true,
			}
		} else if server.letsencrypt {
			m := autocert.Manager{
				Prompt: autocert.AcceptTOS,
			}

			if server.letsencryptDir == "" {
				// then the user passed empty by own will, then I guess user doesnt' want any cache directory
			} else {
				m.Cache = autocert.DirCache(server.letsencryptDir)
			}
			server.TLSConfig = &tls.Config{GetCertificate: m.GetCertificate}
		}
	}

	if server.network == "unix" {
		if errOs := os.Remove(server.Addr); errOs != nil && !os.IsNotExist(errOs) {
			Logger.WithFields(logrus.Fields{
				"Addr":  server.Addr,
				"Error": errOs.Error(),
			}).Fatalln("[NET:UNIX] Unexpected error when trying to remove unix socket file.")
			return nil
		}
		defer func() {
			err := os.Chmod(HttpAddr, UnixFileMode)
			if err != nil {
				Logger.WithFields(logrus.Fields{
					"unixFileMode": server.unixFileMode,
					"Addr":         server.Addr,
					"Error":        err.Error(),
				}).Fatalln("[NET:UNIX] chmod error!")
			}
		}()
	}

	ln, err := graceNet.Listen(server.network, server.Addr)
	if err != nil {
		Logger.WithFields(logrus.Fields{
			"Error": err,
		}).Fatalln("Server Error!")
		return nil
	}
	ln = tcpKeepAliveListener{ln.(*net.TCPListener)}
	if server.TLSConfig != nil {
		ln = tls.NewListener(ln, server.TLSConfig)
	}

	return ln
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (c net.Conn, err error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return
	}
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	return tc, nil
}
func realServeError(err error) error {
	if err != nil && err == http.ErrServerClosed {
		return nil
	}
	return err
}
