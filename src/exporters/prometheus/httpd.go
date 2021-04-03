package main

import (
	"bytes"
	"fmt"
	"goharvest2/share/logger"
	"goharvest2/share/set"
	"net/http"
	//"sort"
	"strings"
)

func (e *Prometheus) StartHttpd(addr, port string) {

	mux := http.NewServeMux()
	mux.HandleFunc("/", e.ServeInfo)
	mux.HandleFunc("/metrics", e.ServeMetrics)

	logger.Warn(e.Prefix+" (httpd)", "Starting server at [:%s]", port)
	server := &http.Server{Addr: ":" + port, Handler: mux}

	if err := server.ListenAndServe(); err != nil {
		logger.Fatal(e.Prefix+" (httpd)", err.Error())
	} else {
		logger.Warn(e.Prefix+" (httpd)", "listening at [:%s]", port)
	}
}

func (e *Prometheus) ServeInfo(w http.ResponseWriter, r *http.Request) {

	logger.Warn(e.Prefix+" (httpd)", "info request (%s) from (%s)", r.RequestURI, r.RemoteAddr)

	body := make([]string, 0)
	num_collectors := 0
	num_objects := 0
	num_metrics := 0
	unique_data := map[string]map[string][]string{}

	for key, data := range e.cache {

		var collector, plugin, object string

		if keys := strings.Split(key, "."); len(keys) == 3 {
			collector = keys[0]
			plugin = keys[1]
			object = keys[2]
		} else {
			continue
		} 
		
		if plugin != "" {
			continue
		}

		metric_names := set.New()

		for _, m := range data {
			if x := strings.Split(string(m), "{"); len(x) >= 2 && x[0] != "" {
				metric_names.Add(x[0])
				}
		}

		num_metrics += metric_names.Size()

		if _, exists := unique_data[collector]; !exists {
			unique_data[collector] = make(map[string][]string)
		}
		unique_data[collector][object] = metric_names.Values()

	}

	for col, per_object := range unique_data {

		objects := make([]string, 0)

		for obj, metric_names := range per_object {

			metrics := make([]string, 0)

			for _, m := range metric_names {
				if m != "" {
					metrics = append(metrics, fmt.Sprintf(metric_template, m))
				}
			}

			objects = append(objects, fmt.Sprintf(object_template, obj, strings.Join(metrics, "\n")))

			num_objects += 1
		}

		body = append(body, fmt.Sprintf(collector_template, col, strings.Join(objects, "\n")))
		num_collectors += 1
	}

	poller := e.Options.Poller
	body_flat := fmt.Sprintf(html_template, poller, poller, poller, num_collectors, num_objects, num_metrics, strings.Join(body, "\n\n"))

	w.WriteHeader(200)
	w.Header().Set("content-type", "text/html")
	w.Write([]byte(body_flat))
}

func (e *Prometheus) ServeMetrics(w http.ResponseWriter, r *http.Request) {

	logger.Warn(e.Prefix+" (httpd) ", "metric request (%s) from (%s)", r.RequestURI, r.RemoteAddr)
	logger.Warn(e.Prefix+" (httpd) ", "loading metrics from %d cached items", len(e.cache))

	sep := []byte("\n")
	var data [][]byte

	count := 0

	for _, m := range e.cache {
		data = append(data, m...)
		count += len(m)
	}

	/*
	e.Metadata.SetValueSS("count", "render", float64(count))

	if md, err := e.Render(e.Metadata); err == nil {
		data = append(data, md...)
	}*/
	
	w.WriteHeader(200)
	w.Header().Set("content-type", "text/plain")
	w.Write(bytes.Join(data, sep))
}
