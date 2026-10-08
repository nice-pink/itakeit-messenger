package classify

import (
	"encoding/json"
	"reflect"
	"sync"
)

// A structured answer is written in schema order, so a model decides `task` before it
// writes `reason`: for a small model the reason is then a justification of a choice
// already made, and a reminder can be flagged false with every time field filled. With
// reason first the model writes its reasoning, then the choice.
//
// The order comes from the Go struct on every backend (the Anthropic SDK and schemaOf
// both reflect on it), so the reordered shape is the same struct with Reason moved to
// the front, built once from Verdict so there is a single list of fields to maintain.
var reasonFirstType = sync.OnceValue(func() reflect.Type {
	vt := reflect.TypeFor[Verdict]()
	fields := make([]reflect.StructField, 0, vt.NumField())
	add := func(f reflect.StructField) {
		fields = append(fields, reflect.StructField{Name: f.Name, Type: f.Type, Tag: f.Tag})
	}
	r, _ := vt.FieldByName("Reason")
	add(r)
	for i := range vt.NumField() {
		if f := vt.Field(i); f.Name != "Reason" {
			add(f)
		}
	}
	return reflect.StructOf(fields)
})

// WithReasonFirst makes the model write its reason before the verdict (see above). It
// costs one sentence of output per message and helps small models; a large one does
// not need it.
func (c *Classifier) WithReasonFirst() *Classifier {
	reasonFirstType() // build now: a Verdict field that cannot be reordered panics at start, not on the first message
	c.reasonFirst = true
	return c
}

// dest returns what the Ask decodes into and a function that reads it as a Verdict.
func (c *Classifier) dest() (any, func() Verdict) {
	if !c.reasonFirst {
		var v Verdict
		return &v, func() Verdict { return v }
	}
	p := reflect.New(reasonFirstType())
	return p.Interface(), func() Verdict { return copyFields[Verdict](p.Elem()) }
}

// EncodeVerdict marshals v in the field order the model is asked to write, which is
// what fine-tuning data must use as the assistant's answer.
func EncodeVerdict(v Verdict, reasonFirst bool) ([]byte, error) {
	if !reasonFirst {
		return json.Marshal(v)
	}
	return json.Marshal(toReasonFirst(v).Interface())
}

// toReasonFirst copies v into a pointer to the reordered struct.
func toReasonFirst(v Verdict) reflect.Value {
	p := reflect.New(reasonFirstType())
	src := reflect.ValueOf(v)
	for i := range src.NumField() {
		p.Elem().FieldByName(src.Type().Field(i).Name).Set(src.Field(i))
	}
	return p
}

// copyFields copies the fields of s, a struct with the same fields as T in any order,
// into a T by name.
func copyFields[T any](s reflect.Value) T {
	var out T
	ov := reflect.ValueOf(&out).Elem()
	for i := range ov.NumField() {
		ov.Field(i).Set(s.FieldByName(ov.Type().Field(i).Name))
	}
	return out
}
