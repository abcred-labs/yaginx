package management

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ID is the API representation of a PostgreSQL bigint. IDs always cross the
// JSON boundary as decimal strings so JavaScript callers never lose precision.
type ID int64

type ACMEOrder struct {
	ID            ID         `json:"id"`
	Domain        string     `json:"domain"`
	Status        string     `json:"status"`
	Attempt       int        `json:"attempt"`
	ErrorClass    string     `json:"errorClass,omitempty"`
	ErrorMessage  string     `json:"errorMessage,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
	NextAttemptAt *time.Time `json:"nextAttemptAt,omitempty"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
}

type ACMEOrderPage struct {
	Records   []ACMEOrder `json:"records"`
	Total     int         `json:"total"`
	PageIndex int         `json:"pageIndex"`
	PageSize  int         `json:"pageSize"`
}

type ACMEOrderLog struct {
	ID         ID        `json:"id"`
	OrderID    ID        `json:"orderId"`
	OccurredAt time.Time `json:"occurredAt"`
	Operation  string    `json:"operation"`
	Status     string    `json:"status"`
	Result     string    `json:"result"`
	ErrorCode  string    `json:"errorCode,omitempty"`
	Message    string    `json:"message,omitempty"`
	NodeID     string    `json:"nodeId,omitempty"`
	Attempt    int       `json:"attempt"`
}

type ACMEOrderLogPage struct {
	Records []ACMEOrderLog `json:"records"`
	Paging  PageCursor     `json:"paging"`
}

func (value ID) String() string { return strconv.FormatInt(int64(value), 10) }

func (value ID) MarshalJSON() ([]byte, error) {
	return json.Marshal(value.String())
}

func (value *ID) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return fmt.Errorf("id must be a decimal string")
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("id must be an int64: %w", err)
		}
		*value = ID(parsed)
		return nil
	}
	return fmt.Errorf("id must be a decimal string")
}

func ParseID(raw string) (ID, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("id must be an int64: %w", err)
	}
	return ID(parsed), nil
}

// Int64Input is used for non-identifier ranges such as traffic timestamps.
// Those values retain their existing number-or-string request compatibility;
// only database identifiers use the strict ID type above.
type Int64Input int64

func (value *Int64Input) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		parsed, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("int64 input is invalid: %w", err)
		}
		*value = Int64Input(parsed)
		return nil
	}
	var number int64
	if err := json.Unmarshal(raw, &number); err != nil {
		return fmt.Errorf("int64 input must be a number or decimal string")
	}
	*value = Int64Input(number)
	return nil
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Remember extends the refresh session from the short default idle/absolute
	// window to the long "remember me" window. See account.Handler.Login.
	Remember bool `json:"remember"`
}

type LoginResult struct {
	Token     string `json:"token"`
	ExpiresIn int    `json:"expiresIn"`
	Name      string `json:"name"`
	UserID    string `json:"userId"`
}

// SessionSummary describes one refresh session for the "active sessions"
// console view. Current marks the session the calling request authenticated
// with so the console can keep the user from revoking their own session by
// mistake without a confirmation step.
type SessionSummary struct {
	ID                ID        `json:"id"`
	Remembered        bool      `json:"remembered"`
	UserAgent         string    `json:"userAgent"`
	ClientIP          string    `json:"clientIp"`
	IssuedAt          time.Time `json:"issuedAt"`
	LastUsedAt        time.Time `json:"lastUsedAt"`
	IdleExpiresAt     time.Time `json:"idleExpiresAt"`
	AbsoluteExpiresAt time.Time `json:"absoluteExpiresAt"`
	Current           bool      `json:"current"`
}

type PasswordRecoveryStartRequest struct {
	AccountEmail string `json:"accountEmail"`
}

type PasswordRecoveryStartResponse struct {
	RecoveryID  string `json:"recoveryId"`
	MaskedEmail string `json:"maskedEmail"`
}

type PasswordRecoveryVerifyEmailRequest struct {
	RecoveryID string `json:"recoveryId"`
	Email      string `json:"email"`
}

type PasswordRecoveryResetRequest struct {
	RecoveryID      string `json:"recoveryId"`
	Code            string `json:"code"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

type Profile struct {
	Email string `json:"email"`
}

type ProfileUpdateRequest struct {
	Email           string `json:"email"`
	OldPassword     string `json:"oldPassword"`
	NewPassword     string `json:"newPassword"`
	ConfirmPassword string `json:"confirmPassword"`
}

type KeyValueString struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type WebsiteHost struct {
	ID                 ID      `json:"id,omitempty"`
	Hostname           string  `json:"hostname"`
	Enabled            bool    `json:"enabled"`
	Role               string  `json:"role"`
	AccessMode         string  `json:"accessMode"`
	RedirectStatusCode int     `json:"redirectStatusCode,omitempty"`
	RedirectTargetHost *string `json:"redirectTargetHost,omitempty"`
	CertificateMode    string  `json:"certificateMode,omitempty"`
	CertificateID      *ID     `json:"certificateId,omitempty"`
	VerificationStatus string  `json:"verificationStatus,omitempty"`
}

type WebsiteProxyRule struct {
	RuleID                     ID      `json:"ruleId"`
	Name                       *string `json:"name"`
	Description                *string `json:"description"`
	IsEnabled                  bool    `json:"isEnabled"`
	PathPattern                *string `json:"pathPattern"`
	IsRegexMatchPattern        bool    `json:"isRegexMatchPattern"`
	Destination                *string `json:"destination"`
	DestinationHost            *string `json:"destinationHost"`
	PreserveOriginalHost       bool    `json:"preserveOriginalHost"`
	AllowInvalidTLSCertificate bool    `json:"allowInvalidTlsCertificate"`
	WebProxy                   *string `json:"webProxy"`
	RemovePrefix               *string `json:"removePrefix"`
	UpstreamPoolID             string  `json:"upstreamPoolId,omitempty"`
	IsFallback                 bool    `json:"isFallback,omitempty"`
}

type UpstreamEndpoint struct {
	Address   string `json:"address"`
	Host      string `json:"host,omitempty"`
	Weight    int    `json:"weight"`
	Priority  int    `json:"priority,omitempty"`
	Enabled   bool   `json:"enabled"`
	TLSVerify *bool  `json:"tlsVerify,omitempty"`
	Proxy     string `json:"proxy,omitempty"`
}
type UpstreamPool struct {
	ID        string             `json:"id"`
	Name      string             `json:"name"`
	Strategy  string             `json:"strategy"`
	Endpoints []UpstreamEndpoint `json:"endpoints"`
}

type SimpleResponse struct {
	Path            *string          `json:"path"`
	ContentType     *string          `json:"contentType"`
	StatusCode      int              `json:"statusCode"`
	ResponseContent *string          `json:"responseContent"`
	ExtraHeaders    []KeyValueString `json:"extraHeaders"`
}

type WebsiteRequestHistoryPolicy struct {
	Mode                string   `json:"mode"`
	SuccessSamplingRate *float64 `json:"successSamplingRate"`
}

// Normalize applies the missing-field default and validates the website API
// contract. A nil policy means inherit, for pre-existing clients and websites.
func (policy *WebsiteRequestHistoryPolicy) Normalize() error {
	if policy == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(policy.Mode)) {
	case "", "inherit":
		policy.Mode = "inherit"
	case "errorsonly":
		policy.Mode = "errorsOnly"
	case "all":
		policy.Mode = "all"
	default:
		return fmt.Errorf("invalid website request history mode %q", policy.Mode)
	}
	if rate := policy.SuccessSamplingRate; rate != nil {
		if math.IsNaN(*rate) || math.IsInf(*rate, 0) || *rate < 0 || *rate > 1 {
			return fmt.Errorf("website request history success sampling rate must be between 0 and 1")
		}
		if policy.Mode != "all" {
			return fmt.Errorf("website request history success sampling rate requires all mode")
		}
	}
	return nil
}

// MaxWebsiteResponseHeaderTimeout bounds the per-site upstream response header
// timeout so one site cannot pin proxy connections indefinitely.
const MaxWebsiteResponseHeaderTimeout = time.Hour

// ParseWebsiteResponseHeaderTimeout validates the optional per-site override.
// An empty value means "inherit proxy.responseHeaderTimeout" and yields zero.
func ParseWebsiteResponseHeaderTimeout(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("website responseHeaderTimeout must be a duration such as 600s: %w", err)
	}
	if value <= 0 || value > MaxWebsiteResponseHeaderTimeout {
		return 0, fmt.Errorf("website responseHeaderTimeout must be greater than 0 and at most %s", MaxWebsiteResponseHeaderTimeout)
	}
	return value, nil
}

type WebsiteUpsertRequest struct {
	ID *ID `json:"id"`
	// ExpectedUpdatedAt is supplied by the editor for optimistic concurrency.
	// Older clients may omit it and retain the legacy unconditional update.
	ExpectedUpdatedAt          *time.Time                   `json:"expectedUpdatedAt,omitempty"`
	Name                       *string                      `json:"name"`
	DefaultHost                *string                      `json:"defaultHost"`
	DefaultDestination         *string                      `json:"defaultDestination"`
	DefaultDestinationHost     *string                      `json:"defaultDestinationHost"`
	WebProxy                   *string                      `json:"webProxy"`
	AuthorizationPolicy        *string                      `json:"authorizationPolicy"`
	PreserveOriginalHost       bool                         `json:"preserveOriginalHost"`
	AllowInvalidTLSCertificate bool                         `json:"allowInvalidTlsCertificate"`
	RedirectHTTPToHTTPS        bool                         `json:"redirectHttpToHttps"`
	IsMaintenanceMode          bool                         `json:"isMaintenanceMode"`
	IPAccessMode               string                       `json:"ipAccessMode,omitempty"`
	IPAccessCIDRs              []string                     `json:"ipAccessCIDRs,omitempty"`
	Hosts                      []string                     `json:"hosts"`
	HostBindings               []WebsiteHost                `json:"hostBindings,omitempty"`
	ProxyRules                 []WebsiteProxyRule           `json:"proxyRules"`
	UpstreamPools              []UpstreamPool               `json:"upstreamPools,omitempty"`
	ProxyTransforms            []KeyValueString             `json:"proxyTransforms"`
	SimpleResponses            []SimpleResponse             `json:"simpleResponses"`
	RequestHistory             *WebsiteRequestHistoryPolicy `json:"requestHistory,omitempty"`
	ResponseHeaderTimeout      string                       `json:"responseHeaderTimeout,omitempty"`
}

type Website struct {
	ID        ID         `json:"id"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	WebsiteUpsertRequest
	HasCertificate bool `json:"hasCertificate"`
}

// WebsiteIPAccessUpsertRequest is the atomic operation used by quick actions
// such as "block this IP for the current site". It deliberately lives beside
// the website write model so the repository can update only the access-policy
// fields without requiring the console to round-trip the entire website.
type WebsiteIPAccessUpsertRequest struct {
	WebsiteID ID     `json:"websiteId"`
	CIDR      string `json:"cidr"`
}

type WebsiteIPAccessRemoveRequest struct {
	WebsiteID ID     `json:"websiteId"`
	CIDR      string `json:"cidr"`
}

type WebsiteIPAccessRule struct {
	WebsiteID ID       `json:"websiteId"`
	CIDR      string   `json:"cidr"`
	Mode      string   `json:"mode"`
	CIDRs     []string `json:"cidrs"`
}

type WebsiteBatchDeleteRequest struct {
	IDs []ID `json:"ids"`
}

type DomainUpsertRequest struct {
	ID                     *ID     `json:"id"`
	Name                   *string `json:"name"`
	AutoRequestCertificate bool    `json:"autoRequestCertificate"`
	IsVerified             bool    `json:"isVerified"`
	FreeCertMessage        *string `json:"freeCertMessage"`
}

type CertificateSummary struct {
	HasCertificate bool    `json:"hasCertificate"`
	NotBefore      int64   `json:"notBefore"`
	NotAfter       int64   `json:"notAfter"`
	Thumbprint     *string `json:"thumbprint"`
}

type Domain struct {
	ID ID `json:"id"`
	DomainUpsertRequest
}

type DomainListItem struct {
	ID                     ID                 `json:"id"`
	Name                   *string            `json:"name"`
	AutoRequestCertificate bool               `json:"autoRequestCertificate"`
	IsVerified             bool               `json:"isVerified"`
	CertSpec               CertificateSummary `json:"certSpec"`
}

type SMTPSettings struct {
	Enabled   bool    `json:"enabled"`
	Host      *string `json:"host"`
	Port      int     `json:"port"`
	UseSSL    bool    `json:"useSsl"`
	UserName  *string `json:"userName"`
	Password  *string `json:"password"`
	FromEmail *string `json:"fromEmail"`
	FromName  *string `json:"fromName"`
	UpdateTS  ID      `json:"updateTs"`
}

type SMTPTestSendRequest struct {
	Email string `json:"email"`
}

type RuntimeSetting struct {
	// Configuration compatibility warnings are local diagnostics, not API fields.
	Warnings  []string       `json:"-"`
	Config    map[string]any `json:"config"`
	Revision  ID             `json:"revision"`
	UpdatedAt *int64         `json:"updatedAt"`
	ApplyMode string         `json:"applyMode"`
}

type RuntimeSettingUpsertRequest struct {
	Config           map[string]any `json:"config"`
	ExpectedRevision *ID            `json:"expectedRevision"`
	Restart          bool           `json:"restart"`
}

type RuntimeSettingStatus struct {
	Revision       ID            `json:"revision"`
	PendingRestart bool          `json:"pendingRestart"`
	Nodes          []GatewayNode `json:"nodes"`
	ApplyMode      string        `json:"applyMode"`
}

type RuntimeAccessContext struct {
	ClientIP      string   `json:"clientIP"`
	AllowedCIDRs  []string `json:"allowedCIDRs"`
	ClientAllowed bool     `json:"clientAllowed"`
}

type NodeRegistrationRequest struct {
	NodeID             string `json:"nodeId"`
	MachineFingerprint string `json:"machineFingerprint"`
	DisplayName        string `json:"displayName"`
	SoftwareVersion    string `json:"softwareVersion"`
	GitSHA             string `json:"gitSHA"`
}

type NodeHeartbeatRequest struct {
	NodeID                    string `json:"nodeId"`
	AppliedRevision           int64  `json:"appliedRevision"`
	ReloadGeneration          int64  `json:"reloadGeneration"`
	ObservedReloadGeneration  int64  `json:"observedReloadGeneration"`
	RestartGeneration         int64  `json:"restartGeneration"`
	ObservedRestartGeneration int64  `json:"observedRestartGeneration"`
	LastSnapshotError         string `json:"lastSnapshotError"`
}

type GatewayNodeSearchRequest struct {
	PageRequest
	Status  string `json:"status"`
	Keyword string `json:"keyword"`
}

type GatewayNodeDisableRequest struct {
	NodeID string `json:"nodeId"`
	Reason string `json:"reason"`
}

type GatewayNodeDeleteRequest struct {
	NodeID string `json:"nodeId"`
}

type GatewayNodeReloadRequest struct {
	NodeID string `json:"nodeId"`
}

type NodeSummary struct {
	NodeID                    string `json:"nodeId"`
	Status                    string `json:"status"`
	NodeNumber                *int16 `json:"nodeNumber"`
	StateReason               string `json:"stateReason"`
	RestartGeneration         int64  `json:"restartGeneration"`
	ObservedRestartGeneration int64  `json:"observedRestartGeneration"`
}

type GatewayNode struct {
	NodeID                    string `json:"nodeId"`
	MachineFingerprint        string `json:"machineFingerprint"`
	DisplayName               string `json:"displayName"`
	Status                    string `json:"status"`
	NodeNumber                *int16 `json:"nodeNumber"`
	StateReason               string `json:"stateReason"`
	SoftwareVersion           string `json:"softwareVersion"`
	GitSHA                    string `json:"gitSha"`
	AppliedRevision           ID     `json:"appliedRevision"`
	ReloadGeneration          ID     `json:"reloadGeneration"`
	ObservedReloadGeneration  ID     `json:"observedReloadGeneration"`
	RestartGeneration         ID     `json:"restartGeneration"`
	ObservedRestartGeneration ID     `json:"observedRestartGeneration"`
	LastSnapshotAt            *int64 `json:"lastSnapshotAt"`
	LastSnapshotError         string `json:"lastSnapshotError"`
	LastHeartbeatAt           *int64 `json:"lastHeartbeatAt"`
	Stale                     bool   `json:"stale"`
	DrainStartedAt            *int64 `json:"drainStartedAt"`
	DisabledAt                *int64 `json:"disabledAt"`
	CreatedAt                 int64  `json:"createdAt"`
	UpdatedAt                 int64  `json:"updatedAt"`
}

type GatewayNodePage = Page[GatewayNode]

type GatewayNodeReloadResult struct {
	NodeID           string `json:"nodeId"`
	ReloadGeneration ID     `json:"reloadGeneration"`
}

type GatewayNodeRestartResult struct {
	NodeID            string `json:"nodeId"`
	RestartGeneration ID     `json:"restartGeneration"`
}

type WebsiteSearchFilter struct {
	Domain         string `json:"domain"`
	Upstream       string `json:"upstream"`
	HasCertificate *bool  `json:"hasCertificate"`
}

// WebsiteSearchRequest is the canonical paged website search payload. The
// filter fields intentionally remain flattened on the JSON wire so existing
// clients can add pageIndex/pageSize without nesting their filters.
type WebsiteSearchRequest struct {
	PageRequest
	WebsiteSearchFilter
}

type WebDomainSearchRequest struct {
	PageRequest
}

type WebsiteBatchDeleteResult struct {
	DeletedIDs  []ID `json:"deletedIds"`
	NotFoundIDs []ID `json:"notFoundIds"`
}

type EmptyResponse struct{}

type IDRequest struct {
	ID string `json:"id"`
}

type IPBlacklistSearchRequest struct {
	PageRequest
	Keyword        string `json:"keyword"`
	IncludeRemoved *bool  `json:"includeRemoved"`
}

type IPBlacklistUpsertRequest struct {
	CIDR  string `json:"cidr"`
	Scope string `json:"scope,omitempty"`
}

type IPProfileName struct {
	Name    string `json:"name,omitempty"`
	ISOCode string `json:"isoCode,omitempty"`
}

type IPProfileCity struct {
	Name      string `json:"name,omitempty"`
	GeoNameID int64  `json:"geoNameId,omitempty"`
}

type IPProfileASN struct {
	Number       int64  `json:"number,omitempty"`
	Organization string `json:"organization,omitempty"`
	Network      string `json:"network,omitempty"`
}

type IPProfileNetwork struct {
	Type           string `json:"type"`
	ISP            string `json:"isp,omitempty"`
	Organization   string `json:"organization,omitempty"`
	ConnectionType string `json:"connectionType,omitempty"`
	UserType       string `json:"userType,omitempty"`
}

type IPProfile struct {
	IP          string            `json:"ip"`
	Country     *IPProfileName    `json:"country,omitempty"`
	Subdivision *IPProfileName    `json:"subdivision,omitempty"`
	City        *IPProfileCity    `json:"city,omitempty"`
	ASN         *IPProfileASN     `json:"asn,omitempty"`
	Network     *IPProfileNetwork `json:"network,omitempty"`
	Traits      map[string]bool   `json:"traits,omitempty"`
}

type IPBlacklistRule struct {
	ID            string    `json:"id"`
	CIDR          string    `json:"cidr"`
	Scope         string    `json:"scope"`
	Profile       IPProfile `json:"profile"`
	BlockedCount  int64     `json:"blockedCount"`
	LastBlockedAt *int64    `json:"lastBlockedAt"`
	CreatedAt     int64     `json:"createdAt"`
	RemovedAt     *int64    `json:"removedAt"`
	IsActive      bool      `json:"isActive"`
}

// IPBlacklistSummary aggregates every blacklist rule, removed ones included,
// because their historical block counts are retained.
type IPBlacklistSummary struct {
	ActiveRules  int64 `json:"activeRules"`
	BlockedTotal int64 `json:"blockedTotal"`
}

// SystemErrorStore is the recorded error journal. It is separate because a
// deployment can run without it, and the search operation answers 501 then.
type SystemErrorStore interface {
	SearchSystemErrors(context.Context, SystemErrorSearchQuery) (SystemErrorPage, error)
	SearchSystemErrorsByTrace(context.Context, string, int) (SystemErrorPage, error)
}
type Account struct {
	ID    int64
	Email string
}
