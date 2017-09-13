package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"encoding/json"
	"github.com/aws/aws-sdk-go/aws/ec2metadata"
	"github.com/aws/aws-sdk-go/aws/session"
)

const USAGE = `Usage: aws-name-server --domain <domain>
                     [ --hostname <hostname>
                       --aws-region us-east-1
                       --aws-access-key-id <access-key>
                       --aws-secret-access-key <secret-key> ]

aws-name-server --domain internal.example.com will serve DNS requests for:

 <name>.internal.example.com          — all ec2 instances tagged with Name=<name>
 <role>.role.internal.example.com     — all ec2 instances tagged with Role=<role>
 <n>.<name>.internal.example.com      — <n>th instance tagged with Name=<name>
 <n>.<role>.role.internal.example.com — <n>th instance tagged with Role=<role>

For more details see https://github.com/danieljimenez/aws-name-server`

const CAPABILITIES = `FATAL

You need to give this program permission to bind to port 53.

Using capabilities (recommended):
 $ sudo setcap cap_net_bind_service=+ep "$(which aws-name-server)"

Just run it as root (not recommended):
 $ sudo aws-name-server
`

func main() {

	domain := flag.String("domain", "", "the domain hierarchy to serve (e.g. aws.example.com)")
	hostname := flag.String("hostname", "", "the public hostname of this server (e.g. ec2-12-34-56-78.compute-1.amazonaws.com)")
	listenAddress := flag.String("listenAddress", ":53", "the public hostname of this server (e.g. ec2-12-34-56-78.compute-1.amazonaws.com)")
	configFile := flag.String("configFile", "/etc/aws-name-server.conf", "path to a JSON file with an array of AWSAccount structs.")
	help := flag.Bool("help", false, "show help")

	flag.Parse()

	if *domain == "" {
		fmt.Println(USAGE)
		log.Fatalf("missing required parameter: --domain")
	} else if *help {
		fmt.Println(USAGE)
		os.Exit(0)
	}

	hostnameFuture := getHostname()
	accounts := getConfig(configFile)

	caches, recordCount, err := NewCaches(accounts, *domain)
	if err != nil {
		log.Fatalf("FATAL: %s", err)
	}

	if *hostname == "" {
		*hostname = <-hostnameFuture
	}

	server := NewNameServer(*domain, *hostname, caches)
	log.Printf("Serving %d DNS records for *.%s from %s%s", recordCount, server.domain, server.hostname, *listenAddress)

	go checkNSRecordMatches(server.domain, server.hostname)
	go server.listenAndServe(*listenAddress, "udp")
	server.listenAndServe(*listenAddress, "tcp")
}

func getConfig(configFile *string) []*AWSAccount {
	var accounts []*AWSAccount

	configFileObj, err := os.Open(*configFile)
	if err != nil {
		log.Printf("WARN: %s", err)
	} else {
		accounts = []*AWSAccount{
			{
				NickName: `json:"NickName"`,
				Arn:      `json:"ARN"`,
				Region:   `json:"Region"`,
			},
		}
	}

	if configFileObj != nil && err == nil {
		jsonParser := json.NewDecoder(configFileObj)
		if err = jsonParser.Decode(&accounts); err != nil {
			log.Fatalf("FATAL: %s", err)
		}
	}

	return accounts
}

func getHostname() chan string {
	result := make(chan string)
	go func() {

		// This can be slow on non-EC2-instances
		mySession, err := session.NewSession()
		if err != nil {
			log.Fatalf("FATAL: %s", err)
		}

		if hostname, err := ec2metadata.New(mySession).GetMetadata("public-hostname"); err == nil {
			result <- string(hostname)
			return
		}

		if hostname, err := os.Hostname(); err == nil {
			result <- hostname
			return
		}

		result <- "localhost"
	}()
	return result
}

// checkNSRecordMatches does a spot check for DNS misconfiguration, and prints a warning
// if using it for DNS is likely to be broken.
func checkNSRecordMatches(domain, hostname string) {

	time.Sleep(1 * time.Second)

	results, err := net.LookupNS(domain)

	if err != nil {
		log.Printf("WARN: No working NS records found for %s", domain)
		log.Printf("WARN: You can still test things using `dig example.%s @%s`, but you won't be able to resolve hosts directly.", domain, hostname)
		log.Printf("WARN: See https://github.com/danieljimenez/aws-name-server for instructions on setting up NS records.")
		return
	}

	matched := false

	for _, record := range results {
		if record.Host == hostname {
			matched = true
		}
	}

	if !matched {
		log.Printf("WARN: The NS record for %s points to: %s", domain, results[0].Host)
		log.Printf("WARN: But --hostname is: %s", hostname)
		log.Printf("WARN: These hostnames must match if you want DNS to work properly.")
		log.Printf("WARN: See https://github.com/danieljimenez/aws-name-server for instructions on NS records.")
	}
}
