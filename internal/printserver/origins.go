package printserver

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// OriginInfo adalah hasil normalisasi satu alamat asal (origin).
type OriginInfo struct {
	Origin string `json:"origin"` // scheme://host[:port] tanpa port standar
	Scheme string `json:"scheme"` // http | https
	Host   string `json:"host"`   // hostname atau IP tanpa port
	Port   int    `json:"port"`   // port efektif (80/443 bila tidak disebut)
	Type   string `json:"type"`   // local | ip | domain | subdomain
}

var hostLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// multiPartSuffixes adalah akhiran domain berlapis yang umum, dipakai untuk
// menentukan apakah sebuah host adalah domain utama atau subdomain.
var multiPartSuffixes = []string{
	"my.id", "co.id", "ac.id", "go.id", "or.id", "sch.id", "web.id", "biz.id",
	"co.uk", "ac.uk", "gov.uk", "org.uk",
	"com.au", "net.au", "org.au", "edu.au", "gov.au",
	"co.jp", "ne.jp", "or.jp", "ac.jp",
	"com.sg", "com.my", "co.nz", "net.nz", "org.nz", "co.za", "com.br", "com.mx",
}

// ParseOrigin memvalidasi dan menormalkan satu alamat asal.
func ParseOrigin(raw string) (OriginInfo, error) {
	var info OriginInfo
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimRight(trimmed, "/")
	if trimmed == "" {
		return info, errors.New("alamat tidak boleh kosong")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return info, errors.New("alamat tidak valid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return info, errors.New("skema harus http atau https")
	}
	if parsed.User != nil {
		return info, errors.New("alamat tidak boleh memuat nama pengguna atau kata sandi")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return info, errors.New("alamat tidak boleh memuat query atau fragment")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return info, errors.New("tidak boleh memuat path; isi origin saja tanpa path")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return info, errors.New("host tidak boleh kosong")
	}
	if err := validateHost(host); err != nil {
		return info, err
	}
	port := 0
	if rawPort := parsed.Port(); rawPort != "" {
		value, err := strconv.Atoi(rawPort)
		if err != nil || value < 1 || value > 65535 {
			return info, errors.New("port harus antara 1 dan 65535")
		}
		port = value
	}
	return composeOrigin(scheme, host, port)
}

// ComposeOrigin merangkai origin dari bagian-bagian terpisah (form UI).
func ComposeOrigin(scheme, host, port string) (OriginInfo, error) {
	scheme = strings.ToLower(strings.TrimSpace(scheme))
	if scheme != "http" && scheme != "https" {
		return OriginInfo{}, errors.New("skema harus http atau https")
	}
	host = strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]/."))
	if host == "" {
		return OriginInfo{}, errors.New("host tidak boleh kosong")
	}
	if err := validateHost(host); err != nil {
		return OriginInfo{}, err
	}
	value := 0
	if rawPort := strings.TrimSpace(port); rawPort != "" {
		parsed, err := strconv.Atoi(rawPort)
		if err != nil || parsed < 1 || parsed > 65535 {
			return OriginInfo{}, errors.New("port harus antara 1 dan 65535")
		}
		value = parsed
	}
	return composeOrigin(scheme, host, value)
}

func composeOrigin(scheme, host string, port int) (OriginInfo, error) {
	defaultPort := 80
	if scheme == "https" {
		defaultPort = 443
	}
	if port == 0 {
		port = defaultPort
	}
	displayHost := host
	if ip := net.ParseIP(host); ip != nil {
		displayHost = "[" + host + "]"
		if ip.To4() != nil {
			displayHost = host
		}
	}
	origin := scheme + "://" + displayHost
	if port != defaultPort {
		origin += ":" + strconv.Itoa(port)
	}
	return OriginInfo{
		Origin: origin,
		Scheme: scheme,
		Host:   host,
		Port:   port,
		Type:   classifyHost(host),
	}, nil
}

func validateHost(host string) error {
	if len(host) > 253 {
		return errors.New("host tidak valid")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || !hostLabelPattern.MatchString(label) {
			return fmt.Errorf("host %q tidak valid", host)
		}
	}
	return nil
}

// classifyHost menentukan tipe host untuk ditampilkan di UI.
func classifyHost(host string) string {
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() {
			return "local"
		}
		return "ip"
	}
	if host == "localhost" {
		return "local"
	}
	labels := strings.Split(host, ".")
	base := 2
	for _, suffix := range multiPartSuffixes {
		if strings.HasSuffix(host, "."+suffix) {
			base = len(strings.Split(suffix, ".")) + 1
			break
		}
	}
	if len(labels) <= base {
		return "domain"
	}
	return "subdomain"
}

// NormalizeOrigins membersihkan dan memvalidasi daftar origin, termasuk
// membuang duplikat tanpa mengubah urutan.
func NormalizeOrigins(list []string) ([]string, error) {
	normalized := make([]string, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, raw := range list {
		info, err := ParseOrigin(raw)
		if err != nil {
			return nil, fmt.Errorf("origin %q: %w", raw, err)
		}
		if seen[info.Origin] {
			continue
		}
		seen[info.Origin] = true
		normalized = append(normalized, info.Origin)
	}
	return normalized, nil
}
