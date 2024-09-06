// Copyright 2020-2024 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package exporter

import (
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/ghodss/yaml"
	"github.com/nats-io/jsm.go/api"
	"github.com/nats-io/jsm.go/monitor"
	"github.com/nats-io/jsm.go/natscontext"
	"github.com/nats-io/nats.go"
	iu "github.com/nats-io/natscli/internal/util"
	"github.com/prometheus/client_golang/prometheus"
)

type Check struct {
	Name       string          `yaml:"name"`
	Kind       string          `yaml:"kind"`
	Context    string          `yaml:"context"`
	Properties json.RawMessage `yaml:"properties"`
}

type Config struct {
	Context string  `yaml:"context"`
	Checks  []Check `yaml:"checks"`
}

type Exporter struct {
	ns     string
	config Config
}

func NewExporter(ns string, f string) (*Exporter, error) {
	cf, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}

	if ns == "" {
		ns = "natscli"
	}

	exporter := &Exporter{
		ns: ns,
	}

	err = yaml.Unmarshal(cf, &exporter.config)
	if err != nil {
		return nil, err
	}

	return exporter, nil
}

// Describe implements prometheus.Collector
func (e *Exporter) Describe(ch chan<- *prometheus.Desc) {
	// we have dynamic metrics so we cant really do this all upfront at the moment
	//
	// according to https://github.com/prometheus/client_golang/issues/47 doing nothing
	// here is the right, if discouraged, thing to do here.
}

// Collect implements prometheus.Collector
func (e *Exporter) Collect(ch chan<- prometheus.Metric) {
	callCheck := func(check *Check, f func(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result)) {
		result := &monitor.Result{Name: check.Name, Check: check.Kind, NameSpace: e.ns, RenderFormat: monitor.NagiosFormat}
		defer result.Collect(ch)

		nctx, err := e.natsContext(check)
		if result.CriticalIfErr(err, "could not load context: %v", err) {
			return
		}

		opts, err := nctx.NATSOptions()
		if result.CriticalIfErr(err, "could not load context: %v", err) {
			return
		}

		f(nctx.ServerURL(), opts, check, result)
		log.Print(result)
	}

	for _, check := range e.config.Checks {
		var f func(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result)

		switch check.Kind {
		case "connection":
			f = e.checkConnection
		case "stream":
			f = e.checkStream
		case "consumer":
			f = e.checkConsumer
		case "message":
			f = e.checkMessage
		case "meta":
			f = e.checkMeta
		case "jetstream":
			f = e.checkJetStream
		case "server":
			f = e.checkServer
		case "kv":
			f = e.checkKv
		case "credential":
			f = e.checkCredential
		default:
			log.Printf("Unknown check kind %s", check.Kind)
			continue
		}

		callCheck(&check, f)
	}
}

func (e *Exporter) natsContext(check *Check) (*natscontext.Context, error) {
	ctxName := check.Context
	if ctxName == "" {
		ctxName = e.config.Context
	}

	if iu.FileExists(ctxName) {
		return natscontext.NewFromFile(ctxName)
	}

	return natscontext.New(ctxName, true)
}

func (e *Exporter) checkCredential(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.CredentialCheckOptions{}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.CheckCredential(result, copts)
	result.CriticalIfErr(err, "check failed: %v", err)

}

func (e *Exporter) checkServer(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.ServerCheckOptions{}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.CheckServer(servers, natsOpts, result, time.Second, copts)
	result.CriticalIfErr(err, "check failed: %v", err)
}

func (e *Exporter) checkJetStream(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.JetStreamAccountOptions{
		MemoryCritical:    -1,
		MemoryWarning:     -1,
		FileWarning:       -1,
		FileCritical:      -1,
		ConsumersCritical: -1,
		ConsumersWarning:  -1,
		StreamCritical:    -1,
		StreamWarning:     -1,
	}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.CheckJetStreamAccount(servers, natsOpts, result, copts)
	result.CriticalIfErr(err, "check failed: %v", err)
}

func (e *Exporter) checkMeta(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.CheckMetaOptions{}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.CheckJetstreamMeta(servers, natsOpts, result, copts)
	result.CriticalIfErr(err, "check failed: %v", err)
}

func (e *Exporter) checkMessage(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.CheckStreamMessageOptions{}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.CheckStreamMessage(servers, natsOpts, result, copts)
	result.CriticalIfErr(err, "check failed: %v", err)
}

func (e *Exporter) checkKv(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.KVCheckOptions{
		ValuesWarning:  -1,
		ValuesCritical: -1,
	}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.CheckKVBucketAndKey(servers, natsOpts, result, copts)
	result.CriticalIfErr(err, "check failed: %v", err)
}

func (e *Exporter) checkConsumer(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.ConsumerHealthCheckOptions{}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.ConsumerHealthCheck(servers, natsOpts, result, copts, api.NewDiscardLogger())
	result.CriticalIfErr(err, "check failed: %v", err)
}

func (e *Exporter) checkStream(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.StreamHealthCheckOptions{}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.StreamHealthCheck(servers, natsOpts, result, copts, api.NewDiscardLogger())
	result.CriticalIfErr(err, "check failed: %v", err)
}

func (e *Exporter) checkConnection(servers string, natsOpts []nats.Option, check *Check, result *monitor.Result) {
	copts := monitor.ConnectionCheckOptions{}
	err := yaml.Unmarshal(check.Properties, &copts)
	if result.CriticalIfErr(err, "invalid properties: %v", err) {
		return
	}

	err = monitor.CheckConnection(servers, natsOpts, 2*time.Second, result, copts)
	result.CriticalIfErr(err, "check failed: %v", err)
}