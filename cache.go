package main

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/sts"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"
)

// The length of time to cache the results of ec2-describe-instances.
// This value is exposed as the TTL of the DNS record (down to a minimum
// of 10 seconds).
const TTL = 1 * time.Minute

// LookupTag represents the type of tag we're caching by.
type LookupTag uint8

const (
	// LOOKUP_NAME for when tag:Name=<value>
	LOOKUP_NAME LookupTag = iota
	// LOOKUP_ROLE for when tag:Role=<value>
	LOOKUP_ROLE
)

// Key is used to cache results in O(1) lookup structures.
type Key struct {
	LookupTag
	string
}

// Record represents the DNS record for one EC2 instance.
type Record struct {
	CName      string
	PublicIP   net.IP
	PrivateIP  net.IP
	ValidUntil time.Time
}

type AWSAccount struct {
	NickName string
	Arn      string
	Region   string
}

// Cache maintains a local cache of data.
// It refreshes every TTL.
type Cache struct {
	awsAccount AWSAccount
	records    map[Key][]*Record
	mutex      sync.RWMutex
	domain     string
}

// NewCaches creates a new array of Cache that uses the provided
// accounts to lookup instances. It starts a goroutine that
// keeps the cache up-to-date.
func NewCaches(accounts []*AWSAccount, domain string) ([]*Cache, int, error) {
	var caches = []*Cache{}
	var recordCount = 0

	// Loop through the child accounts.
	for _, awsAccount := range accounts {
		subAccountCache := &Cache{
			awsAccount: *awsAccount,
			records:    make(map[Key][]*Record),
			domain:     domain,
		}

		if err := subAccountCache.refresh(); err != nil {
			return nil, 0, err
		}

		log.Printf("Scheduling goroutine for %s account", subAccountCache.awsAccount.NickName)
		go func() {
			for range time.Tick(15 * time.Second) {
				err := subAccountCache.refresh()
				if err != nil {
					log.Println("ERROR: " + err.Error())
				}
			}
		}()

		recordCount = recordCount + subAccountCache.Size()
		caches = append(caches, subAccountCache)
	}

	// Now get the data from the account the instance is int.
	instanceAccountCache := &Cache{
		awsAccount: AWSAccount{
			NickName: "main",
			Region:   "us-east-1",
		},
		records: make(map[Key][]*Record),
		domain:  domain,
	}

	if err := instanceAccountCache.refresh(); err != nil {
		return nil, 0, err
	}

	recordCount = recordCount + instanceAccountCache.Size()
	caches = append(caches, instanceAccountCache)

	log.Printf("Scheduling goroutine for %s account", instanceAccountCache.awsAccount.NickName)
	go func() {
		for range time.Tick(15 * time.Second) {
			err := instanceAccountCache.refresh()
			if err != nil {
				log.Println("ERROR: " + err.Error())
			}
		}
	}()

	return caches, recordCount, nil
}

// setRecords updates the cache with a new set of Records
func (cache *Cache) setRecords(records map[Key][]*Record) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.records = records
}

func (cache *Cache) Instances(session *session.Session) (*ec2.DescribeInstancesOutput, error) {
	return ec2.New(session).DescribeInstances(&ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("instance-state-name"),
				Values: []*string{aws.String("running")},
			},
		},
	})
}

func (cache *Cache) Databases(session *session.Session) (*rds.DescribeDBInstancesOutput, error) {
	return rds.New(session).DescribeDBInstances(&rds.DescribeDBInstancesInput{})
}

// allow _ in DNS name
var SANE_DNS_NAME = regexp.MustCompile("^[\\w-]+$")
var SANE_DNS_REPL = regexp.MustCompile("[^\\w-]+")

func sanitize(tag string) string {
	out := strings.ToLower(tag)
	if SANE_DNS_NAME.MatchString(out) {
		return out
	}
	return SANE_DNS_REPL.ReplaceAllString(out, "-")
}

func (cache *Cache) refresh() error {
	if cache.awsAccount.Arn == "" {
		log.Printf("Refreshing data for %s account.", cache.awsAccount.NickName)
	} else {
		log.Printf("Refreshing data for %s account via %s", cache.awsAccount.NickName, cache.awsAccount.Arn)
	}
	records := make(map[Key][]*Record)

	mySession, err := session.NewSession(&aws.Config{
		Region: aws.String(cache.awsAccount.Region),
	})

	if err != nil {
		return err
	}

	// if the cache has an ARN, that means it's tied to a child account, so we'll need to use role switching
	if cache.awsAccount.Arn != "" {
		stsAuth := sts.New(mySession)
		resp, err := stsAuth.AssumeRole(&sts.AssumeRoleInput{
			RoleArn:         &cache.awsAccount.Arn,
			DurationSeconds: aws.Int64(3600),
			RoleSessionName: aws.String("aws-name-server"),
		})

		if err != nil {
			return err
		}

		config := &aws.Config{
			Region: &cache.awsAccount.Region,
			Credentials: credentials.NewStaticCredentials(
				*resp.Credentials.AccessKeyId,
				*resp.Credentials.SecretAccessKey,
				*resp.Credentials.SessionToken,
			),
		}
		mySession, err = session.NewSession(config)
		if err != nil {
			return err
		}
	}

	// do the fetches for all caches

	// database
	databaseResult, err := cache.Databases(mySession)
	if err != nil {
		return err
	}

	databaseRecords := createDatabaseRecords(cache.domain, databaseResult)
	for k, v := range databaseRecords {
		records[k] = v
	}

	// ec2 instances
	instancesResult, err := cache.Instances(mySession)
	if err != nil {
		return err
	}

	instanceRecords := createInstanceRecords(cache.domain, instancesResult)
	for k, v := range instanceRecords {
		records[k] = v
	}

	// update the cache records
	cache.setRecords(records)
	return nil
}

func createInstanceRecords(_ string, instancesResult *ec2.DescribeInstancesOutput) map[Key][]*Record {
	records := make(map[Key][]*Record)
	for _, reservation := range instancesResult.Reservations {
		for _, instance := range reservation.Instances {
			record := Record{}
			record.ValidUntil = time.Now().Add(TTL)

			if instance.PrivateIpAddress != nil {
				record.PrivateIP = net.ParseIP(*instance.PrivateIpAddress)
			}

			// Lookup servers by instance id
			records[Key{LOOKUP_NAME, *instance.InstanceId}] = append(records[Key{LOOKUP_NAME, *instance.InstanceId}], &record)

			for _, tag := range instance.Tags {
				if *tag.Key == "Name" {
					name := sanitize(*tag.Value)
					records[Key{LOOKUP_NAME, name}] = append(records[Key{LOOKUP_NAME, name}], &record)
				}
				if *tag.Key == "Role" {
					role := sanitize(*tag.Value)
					records[Key{LOOKUP_ROLE, role}] = append(records[Key{LOOKUP_ROLE, role}], &record)
				}
			}
		}
	}
	return records
}

func createDatabaseRecords(_ string, databaseResult *rds.DescribeDBInstancesOutput) map[Key][]*Record {
	records := make(map[Key][]*Record)
	for _, r := range databaseResult.DBInstances {
		record := Record{}
		if *r.Endpoint.Address != "" {
			record.CName = *r.Endpoint.Address + "."
			name := sanitize(*r.DBInstanceIdentifier)
			records[Key{LOOKUP_NAME, name}] = append(records[Key{LOOKUP_NAME, name}], &record)
		}
	}
	return records
}

// Lookup a node in the Cache either by Name or Role.
func (cache *Cache) Lookup(tag LookupTag, value string) []*Record {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	return cache.records[Key{tag, value}]
}

func (cache *Cache) Size() int {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()

	return len(cache.records)
}

func (record *Record) TTL(now time.Time) time.Duration {
	if now.After(record.ValidUntil) {
		return 10 * time.Second
	}
	return record.ValidUntil.Sub(now)
}
