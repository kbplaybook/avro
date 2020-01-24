// Code generated by avrogen. DO NOT EDIT.

package cloudEvent

import (
	"github.com/heetch/avro/avrotypegen"
	"time"
)

type Metadata struct {
	Id     string    `json:"id"`
	Source string    `json:"source"`
	Time   time.Time `json:"time"`
}

// AvroRecord implements the avro.AvroRecord interface.
func (Metadata) AvroRecord() avrotypegen.RecordInfo {
	return avrotypegen.RecordInfo{
		Schema: `{"fields":[{"name":"id","type":"string"},{"name":"source","type":"string"},{"name":"time","type":{"logicalType":"timestamp-micros","type":"long"}}],"name":"Metadata","namespace":"avro.apache.org","type":"record"}`,
		Required: []bool{
			0: true,
			1: true,
			2: true,
		},
	}
}

type CloudEvent struct {
	Metadata Metadata
}

// AvroRecord implements the avro.AvroRecord interface.
func (CloudEvent) AvroRecord() avrotypegen.RecordInfo {
	return avrotypegen.RecordInfo{
		Schema: `{"fields":[{"name":"Metadata","type":{"fields":[{"name":"id","type":"string"},{"name":"source","type":"string"},{"name":"time","type":{"logicalType":"timestamp-micros","type":"long"}}],"name":"Metadata","namespace":"avro.apache.org","type":"record"}}],"name":"CloudEvent","namespace":"bar","type":"record"}`,
		Required: []bool{
			0: true,
		},
	}
}
