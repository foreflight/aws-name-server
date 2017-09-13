package main

import (
	"github.com/miekg/dns"
	"log"
	"strconv"
	"strings"
	"time"
)

type NameServer struct {
	domain   string
	hostname string
	caches   []*Cache
}

type response struct {
	*dns.Msg
}

func NewNameServer(domain string, hostname string, caches []*Cache) *NameServer {

	if !strings.HasSuffix(domain, ".") {
		domain += "."
	}
	if !strings.HasSuffix(hostname, ".") {
		hostname += "."
	}

	server := &NameServer{
		domain:   domain,
		hostname: hostname,
		caches:   caches,
	}

	dns.HandleFunc(server.domain, server.handleRequest)

	return server
}

func (s *NameServer) listenAndServe(port string, net string) {
	server := &dns.Server{Addr: port, Net: net}
	if err := server.ListenAndServe(); err != nil {
		if strings.Contains(err.Error(), "permission denied") {
			log.Printf(CAPABILITIES)
		}
		log.Fatalf("%s", err)
	}
}

func (s *NameServer) handleRequest(w dns.ResponseWriter, request *dns.Msg) {
	r := new(dns.Msg)
	r.SetReply(request)
	r.Authoritative = true

	for _, msg := range request.Question {
		log.Printf("%v %#v %v (id=%v)", dns.TypeToString[msg.Qtype], msg.Name, w.RemoteAddr(), request.Id)

		answers := s.Answer(msg)
		if len(answers) > 0 {
			r.Answer = append(r.Answer, answers...)

		} else {
			r.Ns = append(r.Ns, s.SOA(msg))
		}
	}

	w.WriteMsg(r)
}

func (s *NameServer) Answer(msg dns.Question) (answers []dns.RR) {

	if msg.Qtype == dns.TypeNS {
		if msg.Name == s.domain {
			answers = append(answers, &dns.NS{
				Hdr: dns.RR_Header{Name: msg.Name, Rrtype: dns.TypeNS, Class: dns.ClassINET, Ttl: 300},
				Ns:  s.hostname,
			})
		}
		return answers
	}

	if msg.Qtype == dns.TypeSOA {
		if msg.Name == s.domain {
			answers = append(answers, s.SOA(msg))
		}
		return answers
	}

	for _, record := range s.Lookup(msg) {
		ttl := uint32(record.TTL(time.Now()) / time.Second)

		if msg.Qtype == dns.TypeA {
			if record.CName != "" {
				answers = append(answers, &dns.CNAME{
					Hdr:    dns.RR_Header{Name: msg.Name, Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: ttl},
					Target: record.CName,
				})
			} else {
				answers = append(answers, &dns.A{
					Hdr: dns.RR_Header{Name: msg.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: ttl},
					A:   record.PrivateIP,
				})
			}
		}
	}

	return answers
}

func (s *NameServer) Lookup(msg dns.Question) []*Record {
	parts := strings.Split(strings.TrimSuffix(msg.Name, "."+s.domain), ".")

	nth := 0
	tag := LOOKUP_NAME
	hostNick := parts[0:]

	// handle role lookup, e.g. web.role.internal
	if len(parts) > 1 {
		if parts[len(parts)-1] == "role" {
			tag = LOOKUP_ROLE
			parts = parts[:len(parts)-1]
		}
	}

	// handle nth lookup, e.g. 1.web.internal
	if len(parts) > 1 {
		if i, err := strconv.Atoi(parts[0]); err == nil {
			nth = i
			hostNick = parts[1:]
		}
	}

	if len(hostNick) != 1 || hostNick[0] == "" {
		log.Printf("ERROR: badly formed: %s %#v", msg.Name, parts)
		return nil
	}

	var results []*Record
	for e := range s.caches {
		var records = s.caches[e].Lookup(tag, hostNick[0])
		for e := range records {
			var record = records[e]
			results = append(results, record)
		}
	}

	if len(parts) > 1 {
		if nth >= len(results) {
			results = nil
		} else {
			results = results[nth : nth+1]
		}
	} else {
		results = results[:]
	}

	return results
}

func (s *NameServer) SOA(msg dns.Question) dns.RR {
	return &dns.SOA{
		Hdr:     dns.RR_Header{Name: s.domain, Rrtype: dns.TypeSOA, Class: dns.ClassINET, Ttl: 60},
		Ns:      s.hostname,
		Serial:  uint32(time.Now().Unix()),
		Refresh: 86400,
		Retry:   7200,
		Expire:  86400,
		Minttl:  60,
		Mbox:    "hostmaster.",
	}
}
