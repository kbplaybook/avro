// Code generated by avrogen. DO NOT EDIT.

package unionIntVsLong

import (
	"github.com/heetch/avro/avrotypegen"
)

type R struct {
	// Allowed types for interface{} value:
	// 	int64
	// 	int
	// 	string
	F interface{}
}

// AvroRecord implements the avro.AvroRecord interface.
func (R) AvroRecord() avrotypegen.RecordInfo {
	return avrotypegen.RecordInfo{
		Schema: `{"fields":[{"default":1234,"name":"F","type":["long","int","string"]}],"name":"R","type":"record"}`,
		Defaults: []func() interface{}{
			0: func() interface{} {
				return int64(1234)
			},
		},
		Unions: []avrotypegen.UnionInfo{
			0: {
				Type: new(interface{}),
				Union: []avrotypegen.UnionInfo{{
					Type: new(int64),
				}, {
					Type: new(int),
				}, {
					Type: new(string),
				}},
			},
		},
	}
}
