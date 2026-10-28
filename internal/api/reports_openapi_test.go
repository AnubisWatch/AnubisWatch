package api

import (
	"reflect"

	"github.com/AnubisWatch/anubiswatch/internal/sla"
)

// The report schemas are checked against their Go types like the others in
// specSchemaTypes (openapi_schema_test.go).
func init() {
	specSchemaTypes["SLAResponse"] = reflect.TypeOf(SLAResponse{})
	specSchemaTypes["SLAReport"] = reflect.TypeOf(sla.Report{})
	specSchemaTypes["JudgmentExportRecord"] = reflect.TypeOf(exportRecord{})
}
