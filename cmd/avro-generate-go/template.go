package main

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"text/template"

	"github.com/rogpeppe/gogen-avro/v7/parser"
)

type templateParams struct {
	NS *parser.Namespace
}

type headerTemplateParams struct {
	Pkg     string
	Imports []string
}

var templateFuncs = template.FuncMap{
	"typeof":                 typeof,
	"isExportedGoIdentifier": isExportedGoIdentifier,
	"indent":                 indent,
	"recordInfoLiteral":      recordInfoLiteral,
	"doc":                    doc,
	"goType":                 goType,
	"import":                 func(pkg string) string { addImport(pkg); return "" },
}

var headerTemplate = template.Must(
	template.New("").
		Funcs(templateFuncs).
		Delims("«", "»").
		Parse(`
// Code generated by avrogen. DO NOT EDIT.

package «.Pkg»

import (
«range $imp := .Imports»«printf "%q" $imp»
«end»)
`))

var genTemplate = template.Must(
	template.New("").
		Delims("«", "»").
		Funcs(templateFuncs).
		Parse(`
«range $defName, $def  :=.NS.Definitions»
	«- if ne $defName.String .AvroName.String »
		// Alias «$defName» = «.AvroName»
	«- else if eq (typeof .) "RecordDefinition"»
		«- doc "// " .»
		type «.Name» struct {
		«- range $i, $_ := .Fields»
			«- doc "\t// " .»
			«- $type := goType .Type»
			«- if isExportedGoIdentifier .Name»
				«- .GoName» «$type.GoType»
			«- else»
				«- .GoName» «$type.GoType» ` + "`" + `json:«printf "%q" .Name»` + "`" + `
			«- end»
		«end»
		}

		// AvroRecord implements the avro.AvroRecord interface.
		func («.Name») AvroRecord() avrotypegen.RecordInfo {
			return «recordInfoLiteral .»
		}

		// TODO implement MarshalBinary and UnmarshalBinary methods?
	«else if eq (typeof .) "EnumDefinition"»
		«- import "strconv"»
		«- import "fmt"»
		«- doc "// " . -»
		type «.Name» int
		const (
		«- range $i, $sym := .Symbols»
		«$def.SymbolName $sym»«if eq $i 0» «$def.Name» = iota«end»
		«- end»
		)

		var _«.Name»_strings = []string{
		«range $i, $sym := .Symbols»
		«- printf "%q" $sym»,
		«end»}

		// String returns the textual representation of «.Name».
		func (e «.Name») String() string {
			if e < 0 || int(e) >= len(_«.Name»_strings) {
				return "«.Name»(" + strconv.FormatInt(int64(e), 10) + ")"
			}
			return _«.Name»_strings[e]
		}

		// MarshalText implements encoding.TextMarshaler
		// by returning the textual representation of «.Name».
		func (e «.Name») MarshalText() ([]byte, error) {
			if e < 0 || int(e) >= len(_«.Name»_strings) {
				return nil, fmt.Errorf("«.Name» value %d is out of bounds", e)
			}
			return []byte(_«.Name»_strings[e]), nil
		}

		// UnmarshalText implements encoding.TextUnmarshaler
		// by expecting the textual representation of «.Name».
		func (e *«.Name») UnmarshalText(data []byte) error {
			// Note for future: this could be more efficient.
			for i, s := range _«.Name»_strings {
				if string(data) == s {
					*e = «.Name»(i)
					return nil
				}
			}
			return fmt.Errorf("unknown value %q for «.Name»", data)
		}
	«else if eq (typeof .) "FixedDefinition"»
		«- doc "// " . -»
		type «.Name» [«.SizeBytes»]byte
	«else»
		// unknown definition type «printf "%T; name %q" . (typeof .)» .
	«end»
«end»
`[1:]))

func quote(s string) string {
	if !strings.Contains(s, "`") {
		return "`" + s + "`"
	}
	return strconv.Quote(s)
}

type documented interface {
	Doc() string
}

func doc(indentStr string, d interface{}) string {
	if d, ok := d.(documented); ok && d.Doc() != "" {
		return "\n" + indent(d.Doc(), indentStr) + "\n"
	}
	return ""
}

func indent(s, with string) string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return ""
	}
	return with + strings.Replace(s, "\n", "\n"+with, -1)
}

var goIdentifierPat = regexp.MustCompile(`^[A-Z][a-zA-Z_0-9]*$`)

func isExportedGoIdentifier(s string) bool {
	return goIdentifierPat.MatchString(s)
}
func typeof(x interface{}) string {
	if x == nil {
		return "nil"
	}
	t := reflect.TypeOf(x)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if name := t.Name(); name != "" {
		return name
	}
	return ""
}
