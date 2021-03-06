package jaeger

import (
	"fmt"
	"github.com/apache/thrift/lib/go/thrift"
	"github.com/jaegertracing/jaeger/thrift-gen/jaeger"
	apmutil "github.com/justinbarrick/apm-gateway/pkg/apm"
	"github.com/justinbarrick/apm-gateway/pkg/exporters"
	apm "go.elastic.co/apm/model"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"strconv"
	"time"
)

func tagsToMap(jaegerTags []*jaeger.Tag) map[string]string {
	tags := map[string]string{}
	for _, tag := range jaegerTags {
		value := ""

		switch tag.GetVType() {
		case jaeger.TagType_STRING:
			value = tag.GetVStr()
		case jaeger.TagType_DOUBLE:
			value = fmt.Sprintf("%f", tag.GetVDouble())
		case jaeger.TagType_BOOL:
			value = fmt.Sprintf("%t", tag.GetVBool())
		case jaeger.TagType_LONG:
			value = fmt.Sprintf("%d", tag.GetVLong())
		case jaeger.TagType_BINARY:
			value = string(tag.GetVBinary())
		}

		tags[tag.GetKey()] = value
	}
	return tags
}

func serviceToAPM(proc *jaeger.Process) *apm.Service {
	if proc == nil {
		return nil
	}

	return &apm.Service{
		Name: proc.ServiceName,
	}
}

func toAPM(proc *jaeger.Process, span *jaeger.Span) *apm.Transaction {
	sampled := (span.Flags & 1) == 1
	tags := tagsToMap(span.Tags)
	statusCode, _ := strconv.Atoi(tags["http.status_code"])

	return &apm.Transaction{
		ID:        apmutil.SpanId(uint64(span.SpanId)),
		TraceID:   apmutil.TraceId(uint64(span.TraceIdHigh), uint64(span.TraceIdLow)),
		ParentID:  apmutil.SpanId(uint64(span.ParentSpanId)),
		Name:      span.OperationName,
		Timestamp: apm.Time(time.Unix(0, span.StartTime)),
		Duration:  float64(span.Duration),
		Type:      "request",
		Result:    tags["http.status_code"],
		SpanCount: apm.SpanCount{
			Dropped: 0,
			Started: 0,
		},
		Context: &apm.Context{
			Tags: apmutil.TagsToAPM(tags),
			Request: &apm.Request{
				URL: apmutil.TagsToURL(tags),
				Headers: []apm.Header{
					{
						Key:    "User-Agent",
						Values: []string{tags["http.user_agent"]},
					},
				},
				Method: tags["http.method"],
			},
			Service: serviceToAPM(proc),
			Response: &apm.Response{
				StatusCode: statusCode,
			},
		},
		Sampled: &sampled,
	}
}

func decodeJaeger(body io.Reader) (batch jaeger.Batch, err error) {
	data, err := ioutil.ReadAll(body)
	return batch, thrift.NewTDeserializer().Read(&batch, data)
}

type Importer struct {
	exporter exporter.Exporter
}

func (i *Importer) SetExporter(e exporter.Exporter) {
	i.exporter = e
}

func (i *Importer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	spans, err := decodeJaeger(r.Body)
	if err != nil {
		log.Println(err)
	}

	for _, span := range spans.Spans {
		if err := i.exporter.SendToAPM(toAPM(spans.Process, span)); err != nil {
			log.Println(err)
		}
	}

	fmt.Fprintf(w, "Hello, %q", r.URL.Path)
}
