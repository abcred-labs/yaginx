package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"yaginx-gateway/internal/config"
	"yaginx-gateway/internal/database"
	"yaginx-gateway/internal/repository"
)

//go:embed setup.html
var setupPage embed.FS

const setupAddress = "0.0.0.0:9090"
const setupTokenTTL = 15 * time.Minute

type setupAuth struct {
	mu       sync.Mutex
	applyMu  sync.Mutex
	token    string
	expires  time.Time
	consumed bool
	failures map[string]rateLimitEntry
}

type rateLimitEntry struct {
	window time.Time
	count  int
}

func newSetupAuth(now time.Time) (*setupAuth, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("generate setup token: %w", err)
	}
	return &setupAuth{token: hex.EncodeToString(raw[:]), expires: now.Add(setupTokenTTL), failures: make(map[string]rateLimitEntry)}, nil
}

func (a *setupAuth) authorize(r *http.Request, now time.Time) bool {
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	if ip == "" {
		ip = r.RemoteAddr
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	entry := a.failures[ip]
	if entry.window.IsZero() || now.Sub(entry.window) >= time.Minute {
		entry = rateLimitEntry{window: now}
	}
	if entry.count >= 5 {
		a.failures[ip] = entry
		return false
	}
	if a.consumed || !now.Before(a.expires) || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Yaginx-Setup-Token")), []byte(a.token)) != 1 {
		entry.count++
		// Bound memory even when a public listener receives many source addresses.
		if len(a.failures) >= 1024 {
			for key, value := range a.failures {
				if now.Sub(value.window) >= time.Minute {
					delete(a.failures, key)
				}
			}
			if len(a.failures) >= 1024 {
				return false
			}
		}
		a.failures[ip] = entry
		return false
	}
	return true
}

func (a *setupAuth) consume() {
	a.mu.Lock()
	a.consumed = true
	a.mu.Unlock()
}

type setupRequest struct {
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	Database             string `json:"database"`
	Username             string `json:"username"`
	Password             string `json:"password"`
	SSLMode              string `json:"sslMode"`
	BindAddress          string `json:"bindAddress"`
	ManagementPort       int    `json:"managementPort"`
	HTTPPort             int    `json:"httpPort"`
	ProxyHTTPPort        int    `json:"proxyHttpPort"`
	HTTPSPort            int    `json:"httpsPort"`
	ProxyHTTPSPort       int    `json:"proxyHttpsPort"`
	HTTP3Enabled         *bool  `json:"http3Enabled"`
	ACMEEnabled          *bool  `json:"acmeEnabled"`
	ACMEEmail            string `json:"acmeEmail"`
	AdminEmail           string `json:"adminEmail"`
	AdminPassword        string `json:"adminPassword"`
	AdminPasswordConfirm string `json:"adminPasswordConfirm"`
	Action               string `json:"action"`
	Confirm              bool   `json:"confirm"`
}

type setupResponse struct {
	OK             bool     `json:"ok"`
	State          string   `json:"state,omitempty"`
	Message        string   `json:"message,omitempty"`
	Step           string   `json:"step,omitempty"`
	Ready          bool     `json:"ready,omitempty"`
	AccountCreated bool     `json:"accountCreated,omitempty"`
	AccountExists  bool     `json:"accountExists,omitempty"`
	ManagementURL  string   `json:"managementURL,omitempty"`
	Fields         []string `json:"fields,omitempty"`
}

func runSetup(ctx context.Context, configPath, reason string, stdout, stderr io.Writer) int {
	auth, err := newSetupAuth(time.Now())
	if err != nil {
		fmt.Fprintf(stderr, "setup token: %v\n", err)
		return 1
	}
	server := &http.Server{
		Addr:              setupAddress,
		Handler:           setupHandler(configPath, reason, auth),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
	}
	listener, err := net.Listen("tcp4", setupAddress)
	if err != nil {
		fmt.Fprintf(stderr, "setup listener: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "setup mode: listen=%s; open http://<server>:9090/; token=%s; token expires at %s; setup closes after initialization\n", setupAddress, auth.token, auth.expires.Format(time.RFC3339))
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(stderr, "setup server: %v\n", err)
		return 1
	}
	return 0
}

func setupHandler(configPath, reason string, auth *setupAuth) http.Handler {
	return setupHandlerWithApply(configPath, reason, auth, applySetupDatabase, restartSetupService)
}

func setupHandlerWithApply(configPath, reason string, auth *setupAuth, apply func(context.Context, string, setupRequest) error, restart func()) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		page, err := setupPage.ReadFile("setup.html")
		if err != nil {
			http.Error(w, "setup page unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("/api/setup/info", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeSetup(w, r, auth) {
			return
		}
		writeSetupJSON(w, http.StatusOK, setupResponse{OK: true, Message: reason})
	})
	mux.HandleFunc("/api/setup/check", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeSetup(w, r, auth) {
			return
		}
		request, err := decodeSetupRequest(r)
		if err != nil {
			writeSetupError(w, http.StatusBadRequest, err)
			return
		}
		state, err := inspectSetupDatabase(r.Context(), request)
		if err != nil {
			writeSetupError(w, http.StatusBadGateway, err)
			return
		}
		writeSetupJSON(w, http.StatusOK, setupResponse{OK: true, State: string(state), Step: "database", AccountExists: setupAccountExists(r.Context(), request), Message: setupStateMessage(state)})
	})
	mux.HandleFunc("/api/setup/ports", func(w http.ResponseWriter, r *http.Request) {
		if !authorizeSetup(w, r, auth) {
			return
		}
		request, err := decodeSetupRequest(r)
		if err != nil {
			writeSetupError(w, http.StatusBadRequest, err)
			return
		}
		if err := checkSetupPorts(request); err != nil {
			writeSetupError(w, http.StatusConflict, err)
			return
		}
		writeSetupJSON(w, http.StatusOK, setupResponse{OK: true, Message: "all configured listener ports are available"})
	})
	mux.HandleFunc("/api/setup/apply", func(w http.ResponseWriter, r *http.Request) {
		// Serialize schema changes and recheck authorization after waiting, so
		// concurrent requests cannot reuse a successfully consumed token.
		auth.applyMu.Lock()
		defer auth.applyMu.Unlock()
		if !authorizeSetup(w, r, auth) {
			return
		}
		request, err := decodeSetupRequest(r)
		if err != nil {
			writeSetupError(w, http.StatusBadRequest, err)
			return
		}
		if !request.Confirm {
			writeSetupError(w, http.StatusBadRequest, fmt.Errorf("confirmation is required"))
			return
		}
		if err := checkSetupPorts(request); err != nil {
			writeSetupError(w, http.StatusConflict, err)
			return
		}
		if err := apply(r.Context(), configPath, request); err != nil {
			writeSetupError(w, http.StatusBadRequest, err)
			return
		}
		auth.consume()
		writeSetupJSON(w, http.StatusOK, setupResponse{OK: true, State: string(database.StateCurrentV2), Step: "complete", AccountCreated: request.AdminEmail != "", ManagementURL: setupManagementURL(r, request), Message: "database and configuration are ready; waiting for service restart"})
		go restart()
	})
	return mux
}

type setupListenerPort struct {
	name  string
	port  int
	proto string
}

type setupPortError struct {
	fields []string
	err    error
}

func (e *setupPortError) Error() string { return e.err.Error() }
func (e *setupPortError) Unwrap() error { return e.err }

func setupPortFields(name string) []string {
	switch name {
	case "management":
		return []string{"managementPort"}
	case "http":
		return []string{"httpPort"}
	case "proxy_http":
		return []string{"proxyHttpPort"}
	case "https", "http3":
		return []string{"httpsPort"}
	case "proxy_https":
		return []string{"proxyHttpsPort"}
	default:
		return []string{}
	}
}

func checkSetupPorts(request setupRequest) error {
	return checkSetupPortsWithBind(request, bindSetupPort)
}

func bindSetupPort(network, address string) (io.Closer, error) {
	switch network {
	case "udp":
		return net.ListenPacket(network, address)
	case "tcp":
		return net.Listen(network, address)
	default:
		return nil, fmt.Errorf("unsupported listener network %q", network)
	}
}

func checkSetupPortsWithBind(request setupRequest, bind func(network, address string) (io.Closer, error)) error {
	bindAddress := strings.TrimSpace(request.BindAddress)
	if bindAddress == "" {
		bindAddress = "0.0.0.0"
	}
	ports := []setupListenerPort{
		{name: "management", port: effectiveSetupPort(request.ManagementPort, 9090), proto: "tcp"},
		{name: "http", port: effectiveSetupPort(request.HTTPPort, 8080), proto: "tcp"},
		{name: "proxy_http", port: effectiveSetupPort(request.ProxyHTTPPort, 9080), proto: "tcp"},
		{name: "https", port: effectiveSetupPort(request.HTTPSPort, 8443), proto: "tcp"},
		{name: "proxy_https", port: effectiveSetupPort(request.ProxyHTTPSPort, 9443), proto: "tcp"},
	}
	if request.HTTP3Enabled != nil && *request.HTTP3Enabled {
		ports = append(ports, setupListenerPort{name: "http3", port: effectiveSetupPort(request.HTTPSPort, 8443), proto: "udp"})
	}
	seenTCP := make(map[int]string)
	for _, listener := range ports {
		if listener.port < 1 || listener.port > 65535 {
			return &setupPortError{fields: setupPortFields(listener.name), err: fmt.Errorf("listener %s port %d is invalid", listener.name, listener.port)}
		}
		if listener.proto == "tcp" {
			if prior, ok := seenTCP[listener.port]; ok {
				fields := append(setupPortFields(prior), setupPortFields(listener.name)...)
				return &setupPortError{fields: fields, err: fmt.Errorf("listener ports %d are used by both %s and %s", listener.port, prior, listener.name)}
			}
			seenTCP[listener.port] = listener.name
		}
		// setup itself owns 0.0.0.0:9090 until the restart handoff. The
		// management listener is the one intended to take over that socket.
		if listener.name == "management" && listener.port == 9090 {
			continue
		}
		address := net.JoinHostPort(bindAddress, fmt.Sprintf("%d", listener.port))
		connection, err := bind(listener.proto, address)
		if err != nil {
			fields := append([]string{"bindAddress"}, setupPortFields(listener.name)...)
			return &setupPortError{fields: fields, err: fmt.Errorf("listener %s cannot use %s/%s: %w", listener.name, address, listener.proto, err)}
		}
		_ = connection.Close()
	}
	return nil
}

func effectiveSetupPort(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func authorizeSetup(w http.ResponseWriter, r *http.Request, auth *setupAuth) bool {
	if auth.authorize(r, time.Now()) {
		return true
	}
	w.Header().Set("Retry-After", "60")
	writeSetupError(w, http.StatusUnauthorized, fmt.Errorf("setup token is missing, invalid, expired, or rate-limited"))
	return false
}

func setupManagementURL(r *http.Request, request setupRequest) string {
	port := request.ManagementPort
	if port == 0 {
		port = 9090
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, fmt.Sprintf("%d", port))
}

func decodeSetupRequest(r *http.Request) (setupRequest, error) {
	if r.Method != http.MethodPost {
		return setupRequest{}, fmt.Errorf("method not allowed")
	}
	limited := io.LimitReader(r.Body, 64<<10)
	var request setupRequest
	if err := json.NewDecoder(limited).Decode(&request); err != nil {
		return setupRequest{}, fmt.Errorf("invalid request")
	}
	request.Host = strings.TrimSpace(request.Host)
	request.Database = strings.TrimSpace(request.Database)
	request.Username = strings.TrimSpace(request.Username)
	request.SSLMode = strings.TrimSpace(request.SSLMode)
	request.ACMEEmail = strings.TrimSpace(request.ACMEEmail)
	request.AdminEmail = strings.TrimSpace(request.AdminEmail)
	if request.Port == 0 {
		request.Port = 5432
	}
	if request.SSLMode == "" {
		request.SSLMode = "disable"
	}
	if request.Host == "" || request.Database == "" || request.Username == "" || request.Password == "" {
		return setupRequest{}, fmt.Errorf("database host, database name, username, and password are required")
	}
	if request.Port < 1 || request.Port > 65535 {
		return setupRequest{}, fmt.Errorf("database port is invalid")
	}
	if request.SSLMode != "disable" && request.SSLMode != "require" && request.SSLMode != "verify-full" {
		return setupRequest{}, fmt.Errorf("ssl mode must be disable, require, or verify-full")
	}
	if request.Action == "create-database" {
		return setupRequest{}, fmt.Errorf("create-database is no longer supported; create an empty database first")
	}
	if request.AdminEmail != "" && (len(request.AdminPassword) < 5 || request.AdminPassword != request.AdminPasswordConfirm) {
		return setupRequest{}, fmt.Errorf("administrator password must be at least 5 characters and match confirmation")
	}
	if request.AdminEmail != "" {
		parsed, err := mail.ParseAddress(request.AdminEmail)
		if err != nil || parsed.Address != request.AdminEmail {
			return setupRequest{}, fmt.Errorf("administrator email is invalid")
		}
	}
	if request.ACMEEmail != "" {
		parsed, err := mail.ParseAddress(request.ACMEEmail)
		if err != nil || parsed.Address != request.ACMEEmail {
			return setupRequest{}, fmt.Errorf("ACME email is invalid")
		}
	}
	return request, nil
}

func setupDSN(request setupRequest) (string, error) {
	host := request.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(strings.Trim(host, "[]"), fmt.Sprintf("%d", request.Port)), Path: "/" + url.PathEscape(request.Database)}
	u.User = url.UserPassword(request.Username, request.Password)
	query := url.Values{}
	query.Set("sslmode", request.SSLMode)
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func inspectSetupDatabase(ctx context.Context, request setupRequest) (database.State, error) {
	dsn, err := setupDSN(request)
	if err != nil {
		return "", err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	state, err := database.DatabaseState(checkCtx, dsn)
	if err != nil {
		return "", safeSetupError(err, request)
	}
	return state, nil
}

func setupAccountExists(ctx context.Context, request setupRequest) bool {
	dsn, err := setupDSN(request)
	if err != nil {
		return false
	}
	values, err := config.NewRuntimeValues(dsn)
	if err != nil {
		return false
	}
	options, err := runtimePoolOptions(config.RuntimeDocument{Values: values})
	if err != nil {
		return false
	}
	pool, err := database.NewRuntimePool(ctx, dsn, options)
	if err != nil {
		return false
	}
	defer pool.Close()
	count, err := repository.New(pool).CountAccounts(ctx)
	return err == nil && count > 0
}

func applySetupDatabase(ctx context.Context, configPath string, request setupRequest) error {
	dsn, err := setupDSN(request)
	if err != nil {
		return err
	}
	state, err := inspectSetupDatabase(ctx, request)
	if err != nil {
		return err
	}
	if request.Action == "" {
		request.Action = map[database.State]string{database.StateUninitialized: "initialize", database.StateCurrentV2: "migrate", database.StateCurrentV2Pending: "migrate"}[state]
	}
	if state == database.StateUnsupported {
		return fmt.Errorf("database schema is unsupported; automatic setup is refused")
	}
	if state == database.StateUninitialized && request.Action != "initialize" {
		return fmt.Errorf("empty database requires initialize action")
	}
	if state != database.StateUninitialized && request.Action != "migrate" {
		return fmt.Errorf("existing database requires migrate action")
	}
	output := io.Discard
	if request.Action == "initialize" {
		err = database.InitializeV2(ctx, dsn, output)
	} else {
		err = database.MigrateV2(ctx, dsn, output)
	}
	if err != nil {
		return safeSetupError(err, request)
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if state, err := database.DatabaseState(checkCtx, dsn); err != nil || state != database.StateCurrentV2 {
		if err != nil {
			return safeSetupError(fmt.Errorf("runtime database validation: %w", err), request)
		}
		return fmt.Errorf("runtime database validation returned %s", state)
	}
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		var existing map[string]any
		valid := json.Unmarshal(data, &existing) == nil && existing != nil
		if valid {
			if _, loadErr := config.LoadRuntime(configPath); loadErr != nil {
				valid = false
			}
		}
		if !valid {
			backup := configPath + ".pre-setup." + time.Now().UTC().Format("20060102T150405.000000000Z")
			if err := os.WriteFile(backup, data, 0o600); err != nil {
				return fmt.Errorf("backup invalid runtime configuration: %w", err)
			}
		}
	}
	values, err := config.RuntimeValuesForSetup(configPath, dsn)
	if err != nil {
		return err
	}
	if err := applySetupOptions(values, request); err != nil {
		return err
	}
	{
		poolOptions, err := runtimePoolOptions(config.RuntimeDocument{Values: values})
		if err != nil {
			return err
		}
		pool, err := database.NewRuntimePool(ctx, dsn, poolOptions)
		if err != nil {
			return err
		}
		store := repository.New(pool)
		count, err := store.CountAccounts(ctx)
		if err == nil && count == 0 {
			if request.AdminEmail == "" {
				pool.Close()
				return fmt.Errorf("administrator email and password are required for the first account")
			}
			err = store.CreateInitialAccount(ctx, request.AdminEmail, request.AdminPassword)
		}
		pool.Close()
		if err != nil {
			return safeSetupError(err, request)
		}
	}
	if err := config.SaveRuntime(configPath, values); err != nil {
		return err
	}
	return nil
}

func restartSetupService() {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return
	}
	time.Sleep(250 * time.Millisecond)
	command := exec.Command("systemctl", "restart", "yaginx")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	_ = command.Run()
}

func setupStateMessage(state database.State) string {
	switch state {
	case database.StateUninitialized:
		return "empty database; initialization is available"
	case database.StateCurrentV2:
		return "supported v2 schema"
	case database.StateCurrentV2Pending:
		return "current-v2 database has pending migrations"
	case database.StateUnsupported:
		return "unsupported schema; automatic setup is refused"
	default:
		return string(state)
	}
}

func safeSetupError(err error, request setupRequest) error {
	message := err.Error()
	if dsn, dsnErr := setupDSN(request); dsnErr == nil {
		message = strings.ReplaceAll(message, dsn, "[database-dsn]")
	}
	if request.Password != "" {
		message = strings.ReplaceAll(message, request.Password, "[database-password]")
	}
	if request.AdminPassword != "" {
		message = strings.ReplaceAll(message, request.AdminPassword, "[administrator-password]")
	}
	return fmt.Errorf("%s", message)
}

func applySetupOptions(values map[string]any, request setupRequest) error {
	ports := configSection(values, "ports")
	if request.BindAddress != "" {
		ports["bindAddress"] = request.BindAddress
	}
	for key, value := range map[string]int{"management": request.ManagementPort, "http": request.HTTPPort, "proxyHttp": request.ProxyHTTPPort, "https": request.HTTPSPort, "proxyHttps": request.ProxyHTTPSPort} {
		if value > 0 {
			ports[key] = float64(value)
		}
	}
	if request.HTTP3Enabled != nil {
		configSection(configSection(values, "listeners"), "http3")["enabled"] = *request.HTTP3Enabled
	}
	if request.ACMEEnabled != nil {
		// Kept for backwards-compatible setup clients; ACME is always enabled.
		configSection(values, "acme")["enabled"] = true
	}
	acme := configSection(values, "acme")
	if request.ACMEEmail != "" {
		acme["email"] = request.ACMEEmail
	} else if request.AdminEmail != "" {
		acme["email"] = request.AdminEmail
	}
	return config.ValidateRuntimeValues(values)
}

func configSection(values map[string]any, name string) map[string]any {
	section, _ := values[name].(map[string]any)
	if section == nil {
		section = map[string]any{}
		values[name] = section
	}
	return section
}

func writeSetupJSON(w http.ResponseWriter, status int, value setupResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeSetupError(w http.ResponseWriter, status int, err error) {
	response := setupResponse{OK: false, Message: err.Error()}
	var portErr *setupPortError
	if errors.As(err, &portErr) {
		response.Fields = portErr.fields
	}
	writeSetupJSON(w, status, response)
}
