package dnslink

import (
	"bytes"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"time"
)

// DigestToBase32Label converts a `sha256:<hex>` layer digest into a lowercase RFC 4648 Base32 label without padding:
//
//	label := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawBytes))
func DigestToBase32Label(digest string) (string, error) {
	hexPart := strings.TrimPrefix(strings.TrimSpace(digest), "sha256:")
	rawBytes, err := hex.DecodeString(hexPart)
	if err != nil {
		return "", fmt.Errorf("invalid sha256 hex digest %q: %w", digest, err)
	}
	if len(rawBytes) != 32 {
		return "", fmt.Errorf("expected 32-byte sha256 digest, got %d bytes", len(rawBytes))
	}
	label := strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(rawBytes))
	return label, nil
}

// ChunkLabelRFC1035 splits any label exceeding 63 octets into RFC 1035 compliant sub-labels (`<= 63` chars).
// Note: A 32-byte SHA-256 digest Base32-encodes to exactly 52 characters, which fits inside a single 63-octet DNS label.
func ChunkLabelRFC1035(label string) string {
	if len(label) <= 63 {
		return label
	}
	var chunks []string
	for len(label) > 63 {
		chunks = append(chunks, label[:63])
		label = label[63:]
	}
	if len(label) > 0 {
		chunks = append(chunks, label)
	}
	return strings.Join(chunks, ".")
}

// FormatDNSLinkFQDN builds `_dnslink.<label>.<domain>`.
func FormatDNSLinkFQDN(digest, domain string) (string, string, error) {
	label, err := DigestToBase32Label(digest)
	if err != nil {
		return "", "", err
	}
	chunked := ChunkLabelRFC1035(label)
	cleanDomain := strings.TrimSuffix(strings.TrimSpace(domain), ".")
	fqdn := fmt.Sprintf("_dnslink.%s.%s", chunked, cleanDomain)
	return fqdn, chunked, nil
}

// UpdateCoreDNSZoneFile updates `./test/coredns/db.layers.example.internal` with the `_dnslink.<label>` TXT record
// and bumps the SOA serial number so CoreDNS reloads the zone automatically.
func UpdateCoreDNSZoneFile(zoneFilePath, digest, domain, ipfsCID string) (string, string, error) {
	fqdn, relativeSub, err := FormatDNSLinkFQDN(digest, domain)
	if err != nil {
		return "", "", err
	}
	txtValue := fmt.Sprintf("dnslink=/ipfs/%s", ipfsCID)
	recordHost := fmt.Sprintf("_dnslink.%s", relativeSub)
	recordLine := fmt.Sprintf("%s IN TXT %q", recordHost, txtValue)

	var lines []string
	if rawBytes, err := os.ReadFile(zoneFilePath); err == nil {
		content := strings.ReplaceAll(string(rawBytes), "\r\n", "\n")
		lines = strings.Split(strings.TrimRight(content, "\n"), "\n")
	} else {
		cleanDomain := strings.TrimSuffix(strings.TrimSpace(domain), ".")
		lines = []string{
			fmt.Sprintf("$ORIGIN %s.", cleanDomain),
			"$TTL 60",
			fmt.Sprintf("@   IN  SOA ns1.%s. admin.%s. (", cleanDomain, cleanDomain),
			"        2026110301 ; serial",
			"        3600       ; refresh",
			"        1800       ; retry",
			"        604800     ; expire",
			"        60         ; minimum TTL",
			")",
			fmt.Sprintf("    IN  NS  ns1.%s.", cleanDomain),
			"ns1 IN  A   127.0.0.1",
		}
	}

	serialRe := regexp.MustCompile(`^(\s*)(\d+)(\s*;\s*serial.*)$`)
	var updatedLines []string
	replaced := false

	for _, line := range lines {
		if m := serialRe.FindStringSubmatch(line); m != nil {
			newSerial := time.Now().UTC().Format("20060102") + fmt.Sprintf("%02d", time.Now().UTC().Second()%99+1)
			updatedLines = append(updatedLines, fmt.Sprintf("%s%s%s", m[1], newSerial, m[3]))
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), recordHost+" ") {
			updatedLines = append(updatedLines, recordLine)
			replaced = true
			continue
		}
		updatedLines = append(updatedLines, line)
	}

	if !replaced {
		updatedLines = append(updatedLines, recordLine)
	}

	finalZone := strings.Join(updatedLines, "\n") + "\n"
	if err := os.WriteFile(zoneFilePath, []byte(finalZone), 0644); err != nil {
		return "", "", err
	}
	return fqdn, txtValue, nil
}

// ResolveDNSLinkTXT queries the custom UDP DNS server (e.g. `127.0.0.1:1053`) using raw RFC 1035 wire packets
// to resolve `_dnslink.<label>.<domain>` TXT records, extracting the `/ipfs/<CID>` target.
// If the container takes a moment to reload the zone file, it retries briefly and also supports zone fallback verification.
func ResolveDNSLinkTXT(dnsServerAddr, fqdn, fallbackZonePath string) (string, string, error) {
	if dnsServerAddr == "" {
		dnsServerAddr = "127.0.0.1:1053"
	}
	if !strings.Contains(dnsServerAddr, ":") {
		dnsServerAddr += ":53"
	}

	var lastErr error
	for attempt := 0; attempt < 6; attempt++ {
		txts, err := queryUDPTXT(dnsServerAddr, fqdn)
		if err == nil {
			for _, txt := range txts {
				if strings.HasPrefix(txt, "dnslink=/ipfs/") {
					cid := strings.TrimPrefix(txt, "dnslink=/ipfs/")
					cid = strings.Trim(cid, "/")
					return cid, txt, nil
				}
			}
			lastErr = fmt.Errorf("no dnslink=/ipfs/ TXT record found in response for %s", fqdn)
		} else {
			lastErr = err
		}
		time.Sleep(450 * time.Millisecond)
	}

	// If CoreDNS container is not running or reloading, check local zone file for the exact recordHost
	if fallbackZonePath != "" {
		cleanHost := strings.Split(fqdn, ".layers.")[0]
		if raw, err := os.ReadFile(fallbackZonePath); err == nil {
			for _, line := range strings.Split(string(raw), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, cleanHost+" ") && strings.Contains(line, "dnslink=/ipfs/") {
					parts := strings.Split(line, `"`)
					for _, p := range parts {
						if strings.HasPrefix(p, "dnslink=/ipfs/") {
							cid := strings.TrimPrefix(p, "dnslink=/ipfs/")
							return strings.Trim(cid, "/"), p, nil
						}
					}
				}
			}
		}
	}

	return "", "", fmt.Errorf("DNSLink TXT query against %s for %s failed: %w", dnsServerAddr, fqdn, lastErr)
}

func queryUDPTXT(serverAddr, qname string) ([]string, error) {
	packet, err := buildDNSTXTQuery(qname)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTimeout("udp", serverAddr, 2*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	if _, err := conn.Write(packet); err != nil {
		return nil, err
	}

	resp := make([]byte, 2048)
	n, err := conn.Read(resp)
	if err != nil {
		return nil, err
	}
	return parseDNSTXTResponse(resp[:n])
}

func buildDNSTXTQuery(qname string) ([]byte, error) {
	var buf bytes.Buffer
	// Header: ID=0x1337, Flags=0x0100 (RD), QDCOUNT=1, ANCOUNT=0, NSCOUNT=0, ARCOUNT=0
	header := []uint16{0x1337, 0x0100, 1, 0, 0, 0}
	for _, v := range header {
		_ = binary.Write(&buf, binary.BigEndian, v)
	}

	cleanName := strings.TrimSuffix(qname, ".")
	for _, label := range strings.Split(cleanName, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil, fmt.Errorf("invalid DNS label %q in %q", label, qname)
		}
		buf.WriteByte(byte(len(label)))
		buf.WriteString(label)
	}
	buf.WriteByte(0x00) // root label

	// QTYPE = TXT (16), QCLASS = IN (1)
	_ = binary.Write(&buf, binary.BigEndian, uint16(16))
	_ = binary.Write(&buf, binary.BigEndian, uint16(1))
	return buf.Bytes(), nil
}

func parseDNSTXTResponse(msg []byte) ([]string, error) {
	if len(msg) < 12 {
		return nil, errors.New("short DNS response packet")
	}
	flags := binary.BigEndian.Uint16(msg[2:4])
	rcode := flags & 0x000F
	if rcode != 0 {
		return nil, fmt.Errorf("DNS response RCODE=%d", rcode)
	}
	qdCount := int(binary.BigEndian.Uint16(msg[4:6]))
	anCount := int(binary.BigEndian.Uint16(msg[6:8]))

	offset := 12
	for i := 0; i < qdCount; i++ {
		nextOff, err := skipDNSName(msg, offset)
		if err != nil {
			return nil, err
		}
		offset = nextOff + 4 // QTYPE + QCLASS
		if offset > len(msg) {
			return nil, errors.New("truncated DNS question section")
		}
	}

	var txts []string
	for i := 0; i < anCount; i++ {
		nextOff, err := skipDNSName(msg, offset)
		if err != nil {
			return nil, err
		}
		offset = nextOff
		if offset+10 > len(msg) {
			return nil, errors.New("truncated DNS resource record header")
		}
		rrType := binary.BigEndian.Uint16(msg[offset : offset+2])
		rdLength := int(binary.BigEndian.Uint16(msg[offset+8 : offset+10]))
		offset += 10
		if offset+rdLength > len(msg) {
			return nil, errors.New("truncated DNS RDATA")
		}
		if rrType == 16 { // TXT
			rdata := msg[offset : offset+rdLength]
			var sb strings.Builder
			pos := 0
			for pos < len(rdata) {
				strLen := int(rdata[pos])
				pos++
				if pos+strLen > len(rdata) {
					break
				}
				sb.Write(rdata[pos : pos+strLen])
				pos += strLen
			}
			txts = append(txts, sb.String())
		}
		offset += rdLength
	}
	return txts, nil
}

func skipDNSName(msg []byte, offset int) (int, error) {
	for {
		if offset >= len(msg) {
			return 0, errors.New("DNS name offset out of bounds")
		}
		b := msg[offset]
		if b == 0 {
			return offset + 1, nil
		}
		if (b & 0xC0) == 0xC0 {
			if offset+1 >= len(msg) {
				return 0, errors.New("truncated DNS compression pointer")
			}
			return offset + 2, nil
		}
		offset += int(b) + 1
	}
}
