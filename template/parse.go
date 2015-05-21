package template

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/hashicorp/go-multierror"
	"github.com/mitchellh/mapstructure"
)

// rawTemplate is the direct JSON document format of the template file.
// This is what is decoded directly from the file, and then it is turned
// into a Template object thereafter.
type rawTemplate struct {
	MinVersion  string `mapstructure:"min_packer_version"`
	Description string

	Builders      []map[string]interface{}
	Push          map[string]interface{}
	PostProcesors []interface{} `mapstructure:"post-processors"`
	Provisioners  []map[string]interface{}
	Variables     map[string]interface{}
}

// Template returns the actual Template object built from this raw
// structure.
func (r *rawTemplate) Template() (*Template, error) {
	var result Template
	var errs error

	// Let's start by gathering all the builders
	if len(r.Builders) > 0 {
		result.Builders = make(map[string]*Builder, len(r.Builders))
	}
	for i, rawB := range r.Builders {
		var b Builder
		if err := mapstructure.WeakDecode(rawB, &b); err != nil {
			errs = multierror.Append(errs, fmt.Errorf(
				"builder %d: %s", i+1, err))
			continue
		}

		// Set the raw configuration and delete any special keys
		b.Config = rawB
		delete(b.Config, "name")
		delete(b.Config, "type")
		if len(b.Config) == 0 {
			b.Config = nil
		}

		// If there is no type set, it is an error
		if b.Type == "" {
			errs = multierror.Append(errs, fmt.Errorf(
				"builder %d: missing 'type'", i+1))
			continue
		}

		// The name defaults to the type if it isn't set
		if b.Name == "" {
			b.Name = b.Type
		}

		// If this builder already exists, it is an error
		if _, ok := result.Builders[b.Name]; ok {
			errs = multierror.Append(errs, fmt.Errorf(
				"builder %d: builder with name '%s' already exists",
				i+1, b.Name))
			continue
		}

		// Append the builders
		result.Builders[b.Name] = &b
	}

	// Gather all the provisioners
	if len(r.Provisioners) > 0 {
		result.Provisioners = make([]*Provisioner, 0, len(r.Provisioners))
	}
	for i, v := range r.Provisioners {
		var p Provisioner
		if err := r.decoder(&p, nil).Decode(v); err != nil {
			errs = multierror.Append(errs, fmt.Errorf(
				"provisioner %d: %s", i+1, err))
			continue
		}

		// Type is required before any richer validation
		if p.Type == "" {
			errs = multierror.Append(errs, fmt.Errorf(
				"provisioner %d: missing 'type'", i+1))
			continue
		}

		// Copy the configuration
		delete(v, "except")
		delete(v, "only")
		delete(v, "override")
		delete(v, "pause_before")
		delete(v, "type")
		if len(v) > 0 {
			p.Config = v
		}

		// TODO: stuff
		result.Provisioners = append(result.Provisioners, &p)
	}

	// If we have errors, return those with a nil result
	if errs != nil {
		return nil, errs
	}

	return &result, nil
}

func (r *rawTemplate) decoder(
	result interface{},
	md *mapstructure.Metadata) *mapstructure.Decoder {
	d, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		DecodeHook: mapstructure.StringToTimeDurationHookFunc(),
		Metadata:   md,
		Result:     result,
	})
	if err != nil {
		// This really shouldn't happen since we have firm control over
		// all the arguments and they're all unit tested. So we use a
		// panic here to note this would definitely be a bug.
		panic(err)
	}
	return d
}

// Parse takes the given io.Reader and parses a Template object out of it.
func Parse(r io.Reader) (*Template, error) {
	// First, decode the object into an interface{}. We do this instead of
	// the rawTemplate directly because we'd rather use mapstructure to
	// decode since it has richer errors.
	var raw interface{}
	if err := json.NewDecoder(r).Decode(&raw); err != nil {
		return nil, err
	}

	// Create our decoder
	var md mapstructure.Metadata
	var rawTpl rawTemplate
	decoder, err := mapstructure.NewDecoder(&mapstructure.DecoderConfig{
		Metadata: &md,
		Result:   &rawTpl,
	})
	if err != nil {
		return nil, err
	}

	// Do the actual decode into our structure
	if err := decoder.Decode(raw); err != nil {
		return nil, err
	}

	// Build an error if there are unused root level keys
	if len(md.Unused) > 0 {
		sort.Strings(md.Unused)
		for _, unused := range md.Unused {
			err = multierror.Append(err, fmt.Errorf(
				"Unknown root level key in template: '%s'", unused))
		}

		// Return early for these errors
		return nil, err
	}

	// Return the template parsed from the raw structure
	return rawTpl.Template()
}