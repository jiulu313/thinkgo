package core

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	pathpkg "path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/henrylee2cn/thinkgo/core/http2"
	"github.com/henrylee2cn/thinkgo/core/log"
	"github.com/henrylee2cn/thinkgo/core/websocket"
)

type (
	Echo struct {
		prefix                  string
		middleware              []MiddlewareFunc
		http2                   bool
		maxParam                *int
		notFoundHandler         HandlerFunc
		defaultHTTPErrorHandler HTTPErrorHandler
		httpErrorHandler        HTTPErrorHandler
		binder                  Binder
		renderer                Renderer
		pool                    sync.Pool
		debug                   bool
		hook                    http.HandlerFunc
		autoIndex               bool
		logger                  *log.Logger
		router                  *Router
		// @ modified by henrylee2cn 2016.1.22
		blackfile  map[string]bool // 静态文件扫描黑名单
		fileSystem *FileSystem     // 静态文件系统
	}

	Route struct {
		Method  string
		Path    string
		Handler Handler
	}

	HTTPError struct {
		code    int
		message string
	}

	Middleware     interface{}
	MiddlewareFunc func(HandlerFunc) HandlerFunc
	Handler        interface{}
	HandlerFunc    func(*Context) error

	// HTTPErrorHandler is a centralized HTTP error handler.
	HTTPErrorHandler func(error, *Context)

	// Binder is the interface that wraps the Bind method.
	Binder interface {
		Bind(*http.Request, interface{}) error
	}

	binder struct {
	}

	// Validator is the interface that wraps the Validate method.
	Validator interface {
		Validate() error
	}

	// Renderer is the interface that wraps the Render method.
	Renderer interface {
		Render(w io.Writer, name string, data interface{}) error
	}

	// @ modified by henrylee2cn 2016.1.22
	FileSystem struct {
		fs   http.FileSystem // 静态文件系统
		path string          // 静态文件系统url
		dir  string          // 静态文件系统实际路径
	}
)

const (
	// CONNECT HTTP method
	CONNECT = "CONNECT"
	// DELETE HTTP method
	DELETE = "DELETE"
	// GET HTTP method
	GET = "GET"
	// HEAD HTTP method
	HEAD = "HEAD"
	// OPTIONS HTTP method
	OPTIONS = "OPTIONS"
	// PATCH HTTP method
	PATCH = "PATCH"
	// POST HTTP method
	POST = "POST"
	// PUT HTTP method
	PUT = "PUT"
	// TRACE HTTP method
	TRACE = "TRACE"

	// @ modified by henrylee2cn 2016.1.22
	// Web Socket
	SOCKET = "SOCKET"

	//-------------
	// Media types
	//-------------

	ApplicationJSON                  = "application/json"
	ApplicationJSONCharsetUTF8       = ApplicationJSON + "; " + CharsetUTF8
	ApplicationJavaScript            = "application/javascript"
	ApplicationJavaScriptCharsetUTF8 = ApplicationJavaScript + "; " + CharsetUTF8
	ApplicationXML                   = "application/xml"
	ApplicationXMLCharsetUTF8        = ApplicationXML + "; " + CharsetUTF8
	ApplicationForm                  = "application/x-www-form-urlencoded"
	ApplicationProtobuf              = "application/protobuf"
	ApplicationMsgpack               = "application/msgpack"
	TextHTML                         = "text/html"
	TextHTMLCharsetUTF8              = TextHTML + "; " + CharsetUTF8
	TextPlain                        = "text/plain"
	TextPlainCharsetUTF8             = TextPlain + "; " + CharsetUTF8
	MultipartForm                    = "multipart/form-data"

	//---------
	// Charset
	//---------

	CharsetUTF8 = "charset=utf-8"

	//---------
	// Headers
	//---------

	AcceptEncoding     = "Accept-Encoding"
	Authorization      = "Authorization"
	ContentDisposition = "Content-Disposition"
	ContentEncoding    = "Content-Encoding"
	ContentLength      = "Content-Length"
	ContentType        = "Content-Type"
	Location           = "Location"
	Upgrade            = "Upgrade"
	Vary               = "Vary"
	WWWAuthenticate    = "WWW-Authenticate"
	XForwardedFor      = "X-Forwarded-For"
	XRealIP            = "X-Real-IP"
	//-----------
	// Protocols
	//-----------

	WebSocket = "websocket"

	indexPage = "index.html"
)

var (
	methods = [...]string{
		CONNECT,
		DELETE,
		GET,
		HEAD,
		OPTIONS,
		PATCH,
		POST,
		PUT,
		TRACE,
	}

	//--------
	// Errors
	//--------

	UnsupportedMediaType  = errors.New("unsupported media type")
	RendererNotRegistered = errors.New("renderer not registered")
	InvalidRedirectCode   = errors.New("invalid redirect status code")

	//----------------
	// Error handlers
	//----------------

	notFoundHandler = func(c *Context) error {
		return NewHTTPError(http.StatusNotFound)
	}

	methodNotAllowedHandler = func(c *Context) error {
		return NewHTTPError(http.StatusMethodNotAllowed)
	}

	unixEpochTime = time.Unix(0, 0)

	// @ modified by henrylee2cn 2016.1.22
	Log = log.New("echo").SetLevel(log.INFO)
)

// @ modified by henrylee2cn 2016.1.22
// New creates an instance of Echo.
func New() (e *Echo) {
	e = &Echo{
		maxParam:   new(int),
		http2:      true,
		logger:     Log,
		binder:     &binder{},
		fileSystem: new(FileSystem),
		blackfile: map[string]bool{
			".html": true,
		},
		defaultHTTPErrorHandler: func(err error, c *Context) {
			code := http.StatusInternalServerError
			msg := http.StatusText(code)
			if he, ok := err.(*HTTPError); ok {
				code = he.code
				msg = he.message
			}
			if e.debug {
				msg = err.Error()
			}
			if !c.response.committed {
				http.Error(c.response, msg, code)
			}
			e.logger.Error(err)
		},
	}
	e.router = NewRouter(e)
	e.pool.New = func() interface{} {
		return NewContext(nil, new(Response), e)
	}

	e.SetHTTPErrorHandler(e.defaultHTTPErrorHandler)
	return
}

// Router returns router.
func (e *Echo) Router() *Router {
	return e.router
}

// SetLogPrefix sets the prefix for the logger. Default value is `echo`.
func (e *Echo) SetLogPrefix(prefix string) {
	e.logger.SetPrefix(prefix)
}

// SetLogOutput sets the output destination for the logger. Default value is `os.Std*`
func (e *Echo) SetLogOutput(w io.Writer) {
	e.logger.SetOutput(w)
}

// SetLogLevel sets the log level for the logger. Default value is `log.INFO`.
func (e *Echo) SetLogLevel(l log.Level) {
	e.logger.SetLevel(l)
}

// Logger returns the logger instance.
func (e *Echo) Logger() *log.Logger {
	return e.logger
}

// HTTP2 enable/disable HTTP2 support.
func (e *Echo) HTTP2(on bool) {
	e.http2 = on
}

// DefaultHTTPErrorHandler invokes the default HTTP error handler.
func (e *Echo) DefaultHTTPErrorHandler(err error, c *Context) {
	e.defaultHTTPErrorHandler(err, c)
}

// SetHTTPErrorHandler registers a custom Echo.HTTPErrorHandler.
func (e *Echo) SetHTTPErrorHandler(h HTTPErrorHandler) {
	e.httpErrorHandler = h
}

// SetBinder registers a custom binder. It's invoked by Context.Bind().
func (e *Echo) SetBinder(b Binder) {
	e.binder = b
}

// SetRenderer registers an HTML template renderer. It's invoked by Context.Render().
func (e *Echo) SetRenderer(r Renderer) {
	e.renderer = r
}

// @ modified by henrylee2cn 2016.1.22
func (e *Echo) Render(w io.Writer, name string, data interface{}) error {
	return e.renderer.Render(w, name, data)
}

// SetDebug enable/disable debug mode.
func (e *Echo) SetDebug(on bool) {
	e.debug = on
}

// Debug returns debug mode (enabled or disabled).
func (e *Echo) Debug() bool {
	return e.debug
}

// AutoIndex enable/disable automatically creating an index page for the directory.
func (e *Echo) AutoIndex(on bool) {
	e.autoIndex = on
}

// Hook registers a callback which is invoked from `Echo#ServerHTTP` as the first
// statement. Hook is useful if you want to modify response/response objects even
// before it hits the router or any middleware.
func (e *Echo) Hook(h http.HandlerFunc) {
	e.hook = h
}

// @ modified by henrylee2cn 2016.1.22
// 文件服务器排除指定扩展名
func (e *Echo) Blackfile(ext ...string) {
	for _, v := range ext {
		e.blackfile[v] = true
	}
}

// Use adds handler to the middleware chain.
func (e *Echo) Use(m ...Middleware) {
	for _, h := range m {
		e.middleware = append(e.middleware, wrapMiddleware(h))
	}
}

// Connect adds a CONNECT route > handler to the router.
func (e *Echo) Connect(path string, h Handler) {
	e.add(CONNECT, path, h)
}

// Delete adds a DELETE route > handler to the router.
func (e *Echo) Delete(path string, h Handler) {
	e.add(DELETE, path, h)
}

// Get adds a GET route > handler to the router.
func (e *Echo) Get(path string, h Handler) {
	e.add(GET, path, h)
}

// Head adds a HEAD route > handler to the router.
func (e *Echo) Head(path string, h Handler) {
	e.add(HEAD, path, h)
}

// Options adds an OPTIONS route > handler to the router.
func (e *Echo) Options(path string, h Handler) {
	e.add(OPTIONS, path, h)
}

// Patch adds a PATCH route > handler to the router.
func (e *Echo) Patch(path string, h Handler) {
	e.add(PATCH, path, h)
}

// Post adds a POST route > handler to the router.
func (e *Echo) Post(path string, h Handler) {
	e.add(POST, path, h)
}

// Put adds a PUT route > handler to the router.
func (e *Echo) Put(path string, h Handler) {
	e.add(PUT, path, h)
}

// Trace adds a TRACE route > handler to the router.
func (e *Echo) Trace(path string, h Handler) {
	e.add(TRACE, path, h)
}

// Any adds a route > handler to the router for all HTTP methods.
func (e *Echo) Any(path string, h Handler) {
	for _, m := range methods {
		e.add(m, path, h)
	}
}

// @ modified by henrylee2cn 2016.1.22
// Match adds a route > handler to the router for multiple HTTP methods provided.
func (e *Echo) Match(path string, h Handler, method ...string) {
	if len(method) == 0 {
		method = append(method, GET)
	}
	for _, m := range method {
		e.add(m, path, h)
	}
}

// @ modified by henrylee2cn 2016.1.22
// WebSocket adds a WebSocket route > handler to the router.
func (e *Echo) WebSocket(path string, h HandlerFunc) {
	e.Get(path, func(c *Context) (err error) {
		wss := websocket.Server{
			Handler: func(ws *websocket.Conn) {
				c.socket = ws
				c.response.status = http.StatusSwitchingProtocols
				err = h(c)
			},
		}
		wss.ServeHTTP(c.response, c.request)
		return err
	})
	if e.debug {
		e.logger.Notice("%-5s %-25s --> %v", "SOCKET", path, h)
	}
}

// @ modified by henrylee2cn 2016.1.22
func (e *Echo) add(method, path string, h Handler) {
	path = pathpkg.Join(e.prefix, "/", path)
	e.router.Add(method, path, wrapHandler(h), e)
	r := Route{
		Method:  method,
		Path:    path,
		Handler: runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name(),
	}
	e.router.routes = append(e.router.routes, r)
	if e.debug {
		e.logger.Notice("%-5s %-25s --> %v", method, path, h)
	}
}

// Static serves static files from a directory. It's an alias for `Echo.ServeDir`
func (e *Echo) Static(path, dir string) {
	e.ServeDir(path, dir)
}

// @ modified by henrylee2cn 2016.1.22
// ServeDir serves files from a directory.
func (e *Echo) ServeDir(path, dir string) {
	if e.debug {
		e.logger.Notice("	%-25s --> %v", path, dir)
	}
	e.Get(path+"*", func(c *Context) error {
		fs := http.Dir(dir)
		return e.serveFile(fs, c.P(0), c) // Param `_*`
	})
}

// @ modified by henrylee2cn 2016.1.22
// ServeFile serves a file.
func (e *Echo) ServeFile(path, file string) {
	e.Get(path, func(c *Context) error {
		if e.blackfile[filepath.Ext(file)] {
			return NewHTTPError(http.StatusNotFound)
		}
		dir, file := filepath.Split(file)
		fs := http.Dir(dir)
		return e.serveFile(fs, file, c)
	})
}

// @ modified by henrylee2cn 2016.1.22
func (e *Echo) SetFileSystem(path, dir string, fs http.FileSystem) {
	e.fileSystem.fs = fs
	e.fileSystem.path = path
	e.fileSystem.dir = dir
	if e.debug {
		e.logger.Notice("	%-25s --> %v", path, dir)
	}
	e.Get(path+"*", func(c *Context) error {
		return e.serveFile(e.fileSystem.fs, strings.TrimPrefix(c.Request().URL.String(), e.fileSystem.path), c)
	})
}

// @ modified by henrylee2cn 2016.1.22
func (e *Echo) serveFile(fs http.FileSystem, file string, c *Context) (err error) {
	f, err := fs.Open(file)
	if err != nil {
		return NewHTTPError(http.StatusNotFound)
	}
	defer f.Close()

	fi, _ := f.Stat()
	if fi.IsDir() {
		/* NOTE:
		Not checking the Last-Modified header as it caches the response `304` when
		changing differnt directories for the same path.
		*/
		d := f

		// Index file
		file = filepath.Join(file, indexPage)
		f, err = fs.Open(file)
		if err != nil {
			if e.autoIndex {
				// Auto index
				return listDir(d, c)
			}
			return NewHTTPError(http.StatusForbidden)
		}
		fi, _ = f.Stat() // Index file stat
	}

	http.ServeContent(c.response, c.request, fi.Name(), fi.ModTime(), f)
	return
}

func listDir(d http.File, c *Context) (err error) {
	dirs, err := d.Readdir(-1)
	if err != nil {
		return err
	}

	// Create directory index
	w := c.Response()
	w.Header().Set(ContentType, TextHTMLCharsetUTF8)
	fmt.Fprintf(w, "<pre>\n")
	for _, d := range dirs {
		name := d.Name()
		color := "#212121"
		if d.IsDir() {
			color = "#e91e63"
			name += "/"
		}
		fmt.Fprintf(w, "<a href=\"%s\" style=\"color: %s;\">%s</a>\n", name, color, name)
	}
	fmt.Fprintf(w, "</pre>\n")
	return
}

// @ modified by henrylee2cn 2016.1.22
// Group creates a new sub router with prefix. It inherits all properties from
// the parent. Passing middleware overrides parent middleware.
func (e *Echo) Group(prefix string, m ...Middleware) *Group {
	g := &Group{*e}
	g.echo.prefix = pathpkg.Join("/", g.echo.prefix, prefix)
	mw := make([]MiddlewareFunc, len(g.echo.middleware))
	copy(mw, g.echo.middleware)
	g.echo.middleware = mw
	g.Use(m...)
	return g
}

// @ modified by henrylee2cn 2016.1.22
func (e *Echo) Prefix() string {
	return e.prefix
}

// URI generates a URI from handler.
func (e *Echo) URI(h Handler, params ...interface{}) string {
	uri := new(bytes.Buffer)
	pl := len(params)
	n := 0
	hn := runtime.FuncForPC(reflect.ValueOf(h).Pointer()).Name()
	for _, r := range e.router.routes {
		if r.Handler == hn {
			for i, l := 0, len(r.Path); i < l; i++ {
				if r.Path[i] == ':' && n < pl {
					for ; i < l && r.Path[i] != '/'; i++ {
					}
					uri.WriteString(fmt.Sprintf("%v", params[n]))
					n++
				}
				if i < l {
					uri.WriteByte(r.Path[i])
				}
			}
			break
		}
	}
	return uri.String()
}

// URL is an alias for `URI` function.
func (e *Echo) URL(h Handler, params ...interface{}) string {
	return e.URI(h, params...)
}

// Routes returns the registered routes.
func (e *Echo) Routes() []Route {
	return e.router.routes
}

// @ modified by henrylee2cn 2016.1.22
// ServeHTTP implements `http.Handler` interface, which serves HTTP requests.
func (e *Echo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if e.hook != nil {
		e.hook(w, r)
	}
	if r.URL.Path != "/" {
		r.URL.Path = strings.TrimRight(r.URL.Path, "/")
	}

	c := e.pool.Get().(*Context)
	h, e := e.router.Find(r.Method, r.URL.Path, c)
	c.reset(r, w, e)

	// Chain middleware with handler in the end
	for i := len(e.middleware) - 1; i >= 0; i-- {
		h = e.middleware[i](h)
	}

	// Execute chain
	if err := h(c); err != nil {
		e.httpErrorHandler(err, c)
	}

	e.pool.Put(c)
}

// Server returns the internal *http.Server.
func (e *Echo) Server(addr string) *http.Server {
	s := &http.Server{Addr: addr, Handler: e}
	// TODO: Remove in Go 1.6+
	if e.http2 {
		http2.ConfigureServer(s, nil)
	}

	// @ modified by henrylee2cn 2016.1.22
	e.logger.Notice("	%s %s Running on %v", NAME, VERSION, addr)

	return s
}

// Run runs a server.
func (e *Echo) Run(addr string) {
	e.run(e.Server(addr))
}

// RunTLS runs a server with TLS configuration.
func (e *Echo) RunTLS(addr, crtFile, keyFile string) {
	e.run(e.Server(addr), crtFile, keyFile)
}

// RunServer runs a custom server.
func (e *Echo) RunServer(s *http.Server) {
	e.run(s)
}

// RunTLSServer runs a custom server with TLS configuration.
func (e *Echo) RunTLSServer(s *http.Server, crtFile, keyFile string) {
	e.run(s, crtFile, keyFile)
}

func (e *Echo) run(s *http.Server, files ...string) {
	s.Handler = e
	// TODO: Remove in Go 1.6+
	if e.http2 {
		http2.ConfigureServer(s, nil)
	}
	if len(files) == 0 {
		e.logger.Fatal(s.ListenAndServe())
	} else if len(files) == 2 {
		e.logger.Fatal(s.ListenAndServeTLS(files[0], files[1]))
	} else {
		e.logger.Fatal("invalid TLS configuration")
	}
}

func NewHTTPError(code int, msg ...string) *HTTPError {
	he := &HTTPError{code: code, message: http.StatusText(code)}
	if len(msg) > 0 {
		m := msg[0]
		he.message = m
	}
	return he
}

// SetCode sets code.
func (e *HTTPError) SetCode(code int) {
	e.code = code
}

// Code returns code.
func (e *HTTPError) Code() int {
	return e.code
}

// Error returns message.
func (e *HTTPError) Error() string {
	return e.message
}

// wrapMiddleware wraps middleware.
func wrapMiddleware(m Middleware) MiddlewareFunc {
	switch m := m.(type) {
	case MiddlewareFunc:
		return m
	case func(HandlerFunc) HandlerFunc:
		return m
	case HandlerFunc:
		return wrapHandlerFuncMW(m)
	case func(*Context) error:
		return wrapHandlerFuncMW(m)
	case func(http.Handler) http.Handler:
		return func(h HandlerFunc) HandlerFunc {
			return func(c *Context) (err error) {
				m(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					c.response.writer = w
					c.request = r
					err = h(c)
				})).ServeHTTP(c.response.writer, c.request)
				return
			}
		}
	case http.Handler:
		return wrapHTTPHandlerFuncMW(m.ServeHTTP)
	case func(http.ResponseWriter, *http.Request):
		return wrapHTTPHandlerFuncMW(m)
	default:
		panic("unknown middleware")
	}
}

// wrapHandlerFuncMW wraps HandlerFunc middleware.
func wrapHandlerFuncMW(m HandlerFunc) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			if err := m(c); err != nil {
				return err
			}
			return next(c)
		}
	}
}

// wrapHTTPHandlerFuncMW wraps http.HandlerFunc middleware.
func wrapHTTPHandlerFuncMW(m http.HandlerFunc) MiddlewareFunc {
	return func(next HandlerFunc) HandlerFunc {
		return func(c *Context) error {
			if !c.response.committed {
				m.ServeHTTP(c.response.writer, c.request)
			}
			return next(c)
		}
	}
}

// wrapHandler wraps handler.
func wrapHandler(h Handler) HandlerFunc {
	switch h := h.(type) {
	case HandlerFunc:
		return h
	case func(*Context) error:
		return h
	case http.Handler, http.HandlerFunc:
		return func(c *Context) error {
			h.(http.Handler).ServeHTTP(c.response, c.request)
			return nil
		}
	case func(http.ResponseWriter, *http.Request):
		return func(c *Context) error {
			h(c.response, c.request)
			return nil
		}
	default:
		panic("unknown handler")
	}
}

func (binder) Bind(r *http.Request, i interface{}) (err error) {
	ct := r.Header.Get(ContentType)
	err = UnsupportedMediaType
	if strings.HasPrefix(ct, ApplicationJSON) {
		err = json.NewDecoder(r.Body).Decode(i)
	} else if strings.HasPrefix(ct, ApplicationXML) {
		err = xml.NewDecoder(r.Body).Decode(i)
	}
	return
}
