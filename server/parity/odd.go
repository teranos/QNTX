package parity

import (
	"encoding/xml"
	"html"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TEI specifies itself in ODD: an elementSpec per element and a classSpec per
// class of attributes, each saying what it is in English and other languages,
// pinned under Specs/ as the Guidelines keep them (P5/Source/Specs).

// An element is a model. Its columns are its attributes: its own attList, and
// those of every class it is a member of, each with the classes that class
// declares itself a member of, as the Guidelines resolve them.

// The column named content is the element's content model, since a quote's
// words are the element's content and no attribute.

// oddSpec is an elementSpec or a classSpec as its file declares it.
type oddSpec struct {
	XMLName xml.Name
	Ident   string      `xml:"ident,attr"`
	Type    string      `xml:"type,attr"`
	Descs   []oddDesc   `xml:"desc"`
	Members []oddKey    `xml:"classes>memberOf"`
	Content *oddContent `xml:"content"`
	Atts    []oddAttDef `xml:"attList>attDef"`
}

// oddDesc is one desc, in one language, with its markup kept: an attribute
// named in it is written as <att>ana</att>.
type oddDesc struct {
	Lang  string `xml:"http://www.w3.org/XML/1998/namespace lang,attr"`
	Inner string `xml:",innerxml"`
}

type oddKey struct {
	Key string `xml:"key,attr"`
}

type oddContent struct {
	MacroRef *oddKey `xml:"macroRef"`
}

// oddAttDef is one attribute: its name, whether it is required, what it says
// of itself, and its datatype with how many values it takes.
type oddAttDef struct {
	Ident    string    `xml:"ident,attr"`
	Usage    string    `xml:"usage,attr"`
	Descs    []oddDesc `xml:"desc"`
	Datatype *struct {
		MaxOccurs string `xml:"maxOccurs,attr"`
		DataRef   *struct {
			Key  string `xml:"key,attr"`
			Name string `xml:"name,attr"`
		} `xml:"dataRef"`
	} `xml:"datatype"`
}

// ParseODD reads the elements of the ODD files given, by their path in the
// pin, as models with their attributes resolved through the classes pinned
// beside them. A class the pin lacks is the pin's to add, and is named.
func ParseODD(files map[string][]byte) ([]Model, error) {
	classes := map[string]*oddSpec{}
	classFile := map[string]string{}
	var elements []*oddSpec
	for _, name := range slices.Sorted(maps.Keys(files)) {
		spec := new(oddSpec)
		if err := xml.Unmarshal(files[name], spec); err != nil {
			return nil, errors.Wrapf(err, "%s did not read as ODD", name)
		}
		switch {
		case spec.XMLName.Local == "elementSpec":
			elements = append(elements, spec)
		case spec.XMLName.Local == "classSpec" && spec.Type == "atts":
			classes[spec.Ident] = spec
			classFile[spec.Ident] = name
		default:
			return nil, errors.Newf("%s is a %s, and a pin holds elementSpec and classSpec files", name, spec.XMLName.Local)
		}
	}
	var models []Model
	for _, element := range elements {
		model := Model{Name: element.Ident, Says: english(element.Descs)}
		model.Columns = append(model.Columns, contentColumn(element.Content))
		seen := map[string]bool{}
		for _, att := range element.Atts {
			model.Columns = append(model.Columns, columnOf(att, ""))
			seen[att.Ident] = true
		}
		var walk func(class string) error
		walk = func(class string) error {
			spec, ok := classes[class]
			if !ok {
				return errors.Newf("%s is a member of %s, which is not pinned", element.Ident, class)
			}
			for _, att := range spec.Atts {
				if seen[att.Ident] {
					continue
				}
				seen[att.Ident] = true
				model.Columns = append(model.Columns, columnOf(att, classFile[class]+" · "+class+"."+att.Ident))
			}
			// att.global declares itself a member of att.global.responsibility
			// and the rest: a class carries the attributes of the classes it is in.
			for _, m := range spec.Members {
				if !strings.HasPrefix(m.Key, "att.") {
					continue
				}
				if err := walk(m.Key); err != nil {
					return err
				}
			}
			return nil
		}
		for _, m := range element.Members {
			// A model.* class says where an element may appear, not what it carries.
			if !strings.HasPrefix(m.Key, "att.") {
				continue
			}
			if err := walk(m.Key); err != nil {
				return nil, err
			}
		}
		models = append(models, model)
	}
	return models, nil
}

// contentColumn is an element's content model as a column: the macro it
// refers to, or text.
func contentColumn(content *oddContent) Column {
	column := Column{Name: "content", Type: "text"}
	if content != nil && content.MacroRef != nil {
		column.Type = content.MacroRef.Key
	}
	return column
}

// columnOf is one attribute as a column; from is where it was read when it
// comes from a class and not the element's own list.
func columnOf(att oddAttDef, from string) Column {
	column := Column{Name: att.Ident, Required: att.Usage == "req", Says: english(att.Descs)}
	if column.Says != "" {
		column.SaysFrom = from
	}
	if att.Datatype != nil {
		if ref := att.Datatype.DataRef; ref != nil {
			column.Type = ref.Key
			if column.Type == "" {
				column.Type = ref.Name
			}
		}
		if n, err := strconv.Atoi(att.Datatype.MaxOccurs); att.Datatype.MaxOccurs == "unbounded" || (err == nil && n > 1) {
			column.List = true
		}
	}
	return column
}

// english is what a spec says of itself in English, as prose.
func english(descs []oddDesc) string {
	for _, d := range descs {
		if d.Lang == "en" {
			return prose(d.Inner)
		}
	}
	return ""
}

// prose is a desc's markup as its words: a tag such as <att> dropped to the
// text it holds, entities read, and lines joined.
func prose(inner string) string {
	var b strings.Builder
	tag := false
	for _, r := range inner {
		switch {
		case r == '<':
			tag = true
		case r == '>':
			tag = false
		case !tag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(html.UnescapeString(b.String())), " ")
}

// oddAt is the elements of the ODD files of dir as a reference.
func oddAt(dir string, names []string) (Schema, *protocol.Refusal) {
	files := map[string][]byte{}
	for _, name := range names {
		raw, err := pinned.ReadFile(path.Join(dir, name))
		if err != nil {
			return Schema{}, failed("%s did not read: %v", path.Join(dir, name), err)
		}
		files[name] = raw
	}
	models, err := ParseODD(files)
	if err != nil {
		return Schema{}, failed("%s: %v", dir, err)
	}
	return Schema{Models: models, fits: oddFits}, nil
}

// xsdText is the XSD datatypes TEI refers to by name rather than by a teidata
// key, each a string: xml:id is an ID.
var xsdText = []string{"ID", "IDREF", "IDREFS", "NCName", "NMTOKEN", "NMTOKENS", "Name", "anyURI", "string", "token"}

// oddFits is whether a field of a kind can be held in an attribute of a TEI
// datatype, or in an element's content, which is text.
func oddFits(kind protoreflect.Kind, column Column) bool {
	switch kind {
	case protoreflect.StringKind:
		return strings.HasPrefix(column.Type, "teidata.") || strings.HasPrefix(column.Type, "macro.") || column.Type == "text" || slices.Contains(xsdText, column.Type)
	case protoreflect.BoolKind:
		return column.Type == "teidata.truthValue" || column.Type == "teidata.xTruthValue"
	case protoreflect.Int32Kind, protoreflect.Int64Kind, protoreflect.Uint32Kind, protoreflect.Uint64Kind,
		protoreflect.Sint32Kind, protoreflect.Sint64Kind:
		return column.Type == "teidata.count" || column.Type == "teidata.numeric"
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return column.Type == "teidata.numeric" || column.Type == "teidata.probability" || column.Type == "teidata.probCert"
	case protoreflect.EnumKind:
		return column.Type == "teidata.enumerated"
	}
	return false
}
