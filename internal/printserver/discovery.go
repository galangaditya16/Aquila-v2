package printserver

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"
)

const (
	mdnsPort        = 5353
	mdnsPTR  uint16 = 12
	mdnsTXT  uint16 = 16
	mdnsSRV  uint16 = 33
	mdnsA    uint16 = 1
	mdnsAAAA uint16 = 28
)

var mdnsGroup = &net.UDPAddr{IP: net.ParseIP("224.0.0.251"), Port: mdnsPort}

type discoveredPrinter struct {
	Name  string   `json:"name"`
	Model string   `json:"model,omitempty"`
	Host  string   `json:"host"`
	IP    string   `json:"ip,omitempty"`
	Port  int      `json:"port"`
	URI   string   `json:"uri"`
	PDL   []string `json:"pdl,omitempty"`
	State string   `json:"state"`
}

type mdnsRecord struct {
	Name   string
	Type   uint16
	Data   []byte
	Value  string
	Target string
	Port   int
}

type mdnsService struct {
	Name      string
	Secure    bool
	Target    string
	Port      int
	Text      map[string]string
	Addresses []net.IP
}

func discoverIPPPrinters(ctx context.Context) ([]discoveredPrinter, error) {
	conn, err := net.ListenMulticastUDP("udp4", nil, mdnsGroup)
	if err != nil {
		return nil, fmt.Errorf("buka mDNS: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(2500 * time.Millisecond)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = conn.SetDeadline(deadline)
	if _, err := conn.WriteToUDP(mdnsQuery("_ipp._tcp.local", mdnsPTR, "_ipps._tcp.local", mdnsPTR), mdnsGroup); err != nil {
		return nil, fmt.Errorf("kirim pencarian mDNS: %w", err)
	}

	services := make(map[string]*mdnsService)
	addresses := make(map[string][]net.IP)
	buffer := make([]byte, 9000)
	for {
		count, _, readErr := conn.ReadFromUDP(buffer)
		if readErr != nil {
			var netErr net.Error
			if errors.As(readErr, &netErr) && netErr.Timeout() {
				break
			}
			if errors.Is(readErr, net.ErrClosed) {
				break
			}
			return nil, fmt.Errorf("baca jawaban mDNS: %w", readErr)
		}
		records, parseErr := parseMDNSRecords(buffer[:count])
		if parseErr != nil {
			continue
		}
		for _, record := range records {
			owner := normalizeDNSName(record.Name)
			switch record.Type {
			case mdnsPTR:
				serviceType := ""
				if strings.HasPrefix(owner, "_ipp._tcp.") {
					serviceType = "_ipp._tcp.local."
				} else if strings.HasPrefix(owner, "_ipps._tcp.") {
					serviceType = "_ipps._tcp.local."
				}
				if serviceType == "" || record.Value == "" {
					continue
				}
				instance := normalizeDNSName(record.Value)
				service := getMDNSService(services, instance)
				service.Secure = strings.HasPrefix(serviceType, "_ipps.")
				service.Name = printerInstanceName(record.Value, serviceType)
			case mdnsSRV:
				if len(record.Data) < 7 {
					continue
				}
				service := getMDNSService(services, owner)
				service.Port = record.Port
				service.Target = normalizeDNSName(record.Target)
			case mdnsTXT:
				service := getMDNSService(services, owner)
				service.Text = parseMDNSText(record.Data)
			case mdnsA:
				if len(record.Data) == net.IPv4len {
					addresses[owner] = append(addresses[owner], net.IP(append([]byte(nil), record.Data...)))
				}
			case mdnsAAAA:
				if len(record.Data) == net.IPv6len {
					addresses[owner] = append(addresses[owner], net.IP(append([]byte(nil), record.Data...)))
				}
			}
		}
	}

	printersByEndpoint := make(map[string]discoveredPrinter, len(services))
	for _, service := range services {
		if service.Name == "" || service.Port == 0 {
			continue
		}
		service.Addresses = addresses[service.Target]
		host := strings.TrimSuffix(service.Target, ".")
		scheme := "ipp"
		if service.Secure {
			scheme = "ipps"
		}
		resource := service.Text["rp"]
		if resource == "" {
			resource = "ipp/print"
		}
		resource = "/" + strings.TrimLeft(resource, "/")
		printer := discoveredPrinter{
			Name: service.Name, Model: service.Text["ty"], Host: host, Port: service.Port,
			URI: fmt.Sprintf("%s://%s:%d%s", scheme, host, service.Port, resource), State: "detected",
			PDL: splitTXTValues(service.Text["pdl"]),
		}
		if len(service.Addresses) > 0 {
			printer.IP = service.Addresses[0].String()
		}
		key := strings.ToLower(host + ":" + fmt.Sprint(service.Port) + resource)
		current, exists := printersByEndpoint[key]
		if !exists || (strings.HasPrefix(printer.URI, "ipps://") && !strings.HasPrefix(current.URI, "ipps://")) {
			printersByEndpoint[key] = printer
		}
	}
	printers := make([]discoveredPrinter, 0, len(printersByEndpoint))
	for _, printer := range printersByEndpoint {
		printers = append(printers, printer)
	}
	sort.Slice(printers, func(i, j int) bool {
		left := printers[i].Model
		if left == "" {
			left = printers[i].Name
		}
		right := printers[j].Model
		if right == "" {
			right = printers[j].Name
		}
		return strings.ToLower(left) < strings.ToLower(right)
	})
	return printers, nil
}

func getMDNSService(services map[string]*mdnsService, key string) *mdnsService {
	key = normalizeDNSName(key)
	service := services[key]
	if service == nil {
		service = &mdnsService{Text: make(map[string]string)}
		services[key] = service
	}
	return service
}

func mdnsQuery(namesAndTypes ...any) []byte {
	questions := len(namesAndTypes) / 2
	packet := make([]byte, 12)
	binary.BigEndian.PutUint16(packet[4:6], uint16(questions))
	for index := 0; index+1 < len(namesAndTypes); index += 2 {
		name, _ := namesAndTypes[index].(string)
		typeCode, _ := namesAndTypes[index+1].(uint16)
		packet = append(packet, encodeDNSName(name)...)
		question := make([]byte, 4)
		binary.BigEndian.PutUint16(question[0:2], typeCode)
		binary.BigEndian.PutUint16(question[2:4], 1)
		packet = append(packet, question...)
	}
	return packet
}

func encodeDNSName(name string) []byte {
	name = strings.TrimSuffix(name, ".")
	var packet []byte
	for _, label := range strings.Split(name, ".") {
		if len(label) == 0 || len(label) > 63 {
			return nil
		}
		packet = append(packet, byte(len(label)))
		packet = append(packet, []byte(label)...)
	}
	return append(packet, 0)
}

func parseMDNSRecords(packet []byte) ([]mdnsRecord, error) {
	if len(packet) < 12 {
		return nil, errors.New("paket DNS terlalu pendek")
	}
	questionCount := int(binary.BigEndian.Uint16(packet[4:6]))
	recordCount := int(binary.BigEndian.Uint16(packet[6:8])) + int(binary.BigEndian.Uint16(packet[8:10])) + int(binary.BigEndian.Uint16(packet[10:12]))
	offset := 12
	for range questionCount {
		_, next, err := readDNSName(packet, offset)
		if err != nil || next+4 > len(packet) {
			return nil, errors.New("bagian pertanyaan DNS tidak valid")
		}
		offset = next + 4
	}
	records := make([]mdnsRecord, 0, recordCount)
	for range recordCount {
		name, next, err := readDNSName(packet, offset)
		if err != nil || next+10 > len(packet) {
			return nil, errors.New("record DNS tidak valid")
		}
		typeCode := binary.BigEndian.Uint16(packet[next : next+2])
		dataLength := int(binary.BigEndian.Uint16(packet[next+8 : next+10]))
		dataStart := next + 10
		dataEnd := dataStart + dataLength
		if dataEnd > len(packet) {
			return nil, errors.New("data record DNS tidak lengkap")
		}
		record := mdnsRecord{Name: name, Type: typeCode, Data: append([]byte(nil), packet[dataStart:dataEnd]...)}
		if typeCode == mdnsPTR {
			record.Value, _, err = readDNSName(packet, dataStart)
			if err != nil {
				return nil, err
			}
		} else if typeCode == mdnsSRV {
			if dataLength < 7 {
				return nil, errors.New("record SRV DNS terlalu pendek")
			}
			record.Port = int(binary.BigEndian.Uint16(packet[dataStart+4 : dataStart+6]))
			record.Target, _, err = readDNSName(packet, dataStart+6)
			if err != nil {
				return nil, err
			}
		}
		records = append(records, record)
		offset = dataEnd
	}
	return records, nil
}

func readDNSName(packet []byte, start int) (string, int, error) {
	if start < 0 || start >= len(packet) {
		return "", start, errors.New("offset nama DNS tidak valid")
	}
	labels := make([]string, 0, 4)
	offset, nextOffset, jumped := start, start, false
	visited := make(map[int]bool)
	for steps := 0; steps < 128; steps++ {
		if offset >= len(packet) {
			return "", start, errors.New("nama DNS terpotong")
		}
		length := int(packet[offset])
		if length&0xc0 == 0xc0 {
			if offset+1 >= len(packet) {
				return "", start, errors.New("pointer nama DNS terpotong")
			}
			pointer := ((length & 0x3f) << 8) | int(packet[offset+1])
			if pointer >= len(packet) || visited[pointer] {
				return "", start, errors.New("pointer nama DNS berulang atau tidak valid")
			}
			visited[pointer] = true
			if !jumped {
				nextOffset = offset + 2
			}
			jumped = true
			offset = pointer
			continue
		}
		if length&0xc0 != 0 || length > 63 {
			return "", start, errors.New("label nama DNS tidak valid")
		}
		offset++
		if length == 0 {
			if !jumped {
				nextOffset = offset
			}
			return strings.Join(labels, ".") + ".", nextOffset, nil
		}
		if offset+length > len(packet) {
			return "", start, errors.New("label nama DNS terpotong")
		}
		labels = append(labels, string(packet[offset:offset+length]))
		offset += length
		if !jumped {
			nextOffset = offset
		}
	}
	return "", start, errors.New("nama DNS terlalu dalam")
}

func parseMDNSText(data []byte) map[string]string {
	values := make(map[string]string)
	for offset := 0; offset < len(data); {
		length := int(data[offset])
		offset++
		if offset+length > len(data) {
			break
		}
		value := string(data[offset : offset+length])
		offset += length
		key, item, found := strings.Cut(value, "=")
		if found {
			values[strings.ToLower(key)] = item
		}
	}
	return values
}

func printerInstanceName(instance, serviceType string) string {
	instance = strings.TrimSuffix(instance, ".")
	suffix := strings.TrimSuffix(serviceType, ".")
	if strings.HasSuffix(instance, "."+suffix) {
		instance = strings.TrimSuffix(instance, "."+suffix)
	}
	return strings.TrimSpace(strings.TrimSuffix(instance, "."))
}

func normalizeDNSName(name string) string {
	return strings.ToLower(strings.TrimSuffix(name, ".")) + "."
}

func splitTXTValues(value string) []string {
	if value == "" {
		return nil
	}
	values := strings.Split(value, ",")
	for index := range values {
		values[index] = strings.TrimSpace(values[index])
	}
	return values
}
