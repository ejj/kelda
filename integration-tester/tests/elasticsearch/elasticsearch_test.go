package main

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kelda/kelda/db"
	"github.com/kelda/kelda/integration-tester/util"
)

func TestElasticsearch(t *testing.T) {
	clnt, _, err := util.GetDefaultDaemonClient()
	if err != nil {
		t.Fatalf("couldn't get api client: %s", err)
	}
	defer clnt.Close()

	containers, err := clnt.QueryContainers()
	if err != nil {
		t.Fatalf("couldn't query containers: %s", err)
	}

	machines, err := clnt.QueryMachines()
	if err != nil {
		t.Fatalf("couldn't query machines: %s", err)
	}

	endpoints := getElasticsearchEndpoints(machines, containers)
	if err := testElasticsearch(endpoints); err != nil {
		t.Fatal(err)
	}
}

func getElasticsearchEndpoints(machines []db.Machine, containers []db.Container) (
	endpoints []string) {
	ipMap := make(map[string]string)
	for _, m := range machines {
		ipMap[m.PrivateIP] = m.PublicIP
	}

	for _, c := range containers {
		if strings.Contains(c.Image, "elasticsearch") {
			endpoints = append(endpoints, "http://"+ipMap[c.Minion]+":9200")
		}
	}
	return endpoints
}

// testElasticsearch creates an index, seeds it with data, then tries to query it.
func testElasticsearch(endpoints []string) error {
	if len(endpoints) == 0 {
		return errors.New("no Elasticsearch endpoints")
	}

	fmt.Println("Creating index..")
	_, err := httpDo("POST", random(endpoints)+"/shakespeare",
		`{
		 "mappings" : {
		  "_default_" : {
		   "properties" : {
			"speaker" : {"type": "string", "index" : "not_analyzed" },
			"play_name" : {"type": "string", "index" : "not_analyzed" },
			"line_id" : { "type" : "integer" },
			"speech_number" : { "type" : "integer" }
		   }
		  }
		 }
		}`)
	if err != nil {
		return fmt.Errorf("create index: %s", err.Error())
	}

	fmt.Println("Seeding index..")
	_, err = httpDo("POST", random(endpoints)+"/_bulk",
		`{"index":{"_index":"shakespeare","_type":"act","_id":0}}
		{"line_id":1,"play_name":"Henry IV","speech_number":"", `+
			`"line_number":"","speaker":"","text_entry":"ACT I"}
		{"index":{"_index":"shakespeare","_type":"scene","_id":1}}
		{"line_id":2,"play_name":"Henry IV","speech_number":"", `+
			`"line_number":"","speaker":"","text_entry":"SCENE I. London."}
		{"index":{"_index":"shakespeare","_type":"line","_id":2}}
		{"line_id":3,"play_name":"Henry IV","speech_number":"",`+
			`"line_number":"","speaker":"","text_entry":`+
			`"Enter KING HENRY, LORD JOHN OF LANCASTER,"}
		{"index":{"_index":"shakespeare","_type":"line","_id":3}}
		{"line_id":4,"play_name":"Henry IV","speech_number":1, `+
			`"line_number":"1.1.1","speaker":"KING HENRY IV", `+
			`"text_entry":"So shaken as we are, so wan "}
		{"index":{"_index":"shakespeare","_type":"line","_id":4}}`)
	if err != nil {
		return fmt.Errorf("seed index: %s", err.Error())
	}

	fmt.Println("Sleeping 30 seconds before querying data")
	time.Sleep(30 * time.Second)

	for i := 0; i < 10; i++ {
		fmt.Println("Trying query..")
		query := "/_search?q=play_name:Henry%20IV"
		body, err := httpDo("GET", random(endpoints)+query, "")
		if err != nil {
			return fmt.Errorf("query: %s", err.Error())
		}
		if !strings.Contains(body, `"hits":{"total":4`) {
			return errors.New("query returned wrong results")
		}
	}

	return nil
}

func httpDo(verb, endpoint, payload string) (string, error) {
	req, err := http.NewRequest(verb, endpoint, bytes.NewBufferString(payload))
	if err != nil {
		return "", err
	}
	fmt.Printf("\nRequest: %+v\n", req)

	c := &http.Client{}
	resp, err := c.Do(req)
	if err != nil {
		return "", err
	}
	fmt.Printf("Response: %+v\n", resp)

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	fmt.Printf("Body: %+v\n\n", string(body))
	return string(body), nil
}

func random(endpoints []string) string {
	return endpoints[rand.Intn(len(endpoints))]
}
