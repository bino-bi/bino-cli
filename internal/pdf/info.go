package pdf

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Info holds values for the PDF document information dictionary.
// Empty values are not written.
type Info struct {
	Title    string
	Author   string
	Subject  string
	Keywords []string
}

func (info Info) properties() map[string]string {
	props := map[string]string{}
	for key, val := range map[string]string{"Title": info.Title, "Author": info.Author, "Subject": info.Subject} {
		if v := strings.TrimSpace(val); v != "" {
			props[key] = v
		}
	}
	keywords := make([]string, 0, len(info.Keywords))
	for _, k := range info.Keywords {
		if k = strings.TrimSpace(k); k != "" {
			keywords = append(keywords, k)
		}
	}
	if len(keywords) > 0 {
		props["Keywords"] = strings.Join(keywords, ", ")
	}
	return props
}

// SetInfo writes the non-empty values of info into the document information
// dictionary of the PDF at path. Without values the file is left untouched.
//
// The change is appended as an incremental update: pdfcpu's full writer
// emits objects in random order, which would break reproducible builds.
func SetInfo(path string, info Info) (err error) {
	props := info.properties()
	if len(props) == 0 {
		return nil
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("set pdf info: %w", err)
	}
	defer func() { err = errors.Join(err, f.Close()) }()

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	ctx, err := api.ReadContext(f, conf)
	if err != nil {
		return fmt.Errorf("set pdf info: read: %w", err)
	}
	if ctx.XRefTable.Version() < model.V14 {
		return fmt.Errorf("set pdf info: pdf version %s does not support incremental updates", ctx.VersionString())
	}

	// Keep the xref style of the file: a reader may reject a table section
	// whose /Prev points at an xref stream.
	ctx.WriteXRefStream = ctx.Read.UsingXRefStreams
	ctx.WriteObjectStream = ctx.Read.UsingObjectStreams
	ctx.Write.Increment = true
	ctx.Write.Offset = ctx.Read.FileSize

	if err := pdfcpu.PropertiesAdd(ctx, props); err != nil {
		return fmt.Errorf("set pdf info: %w", err)
	}
	if err := api.WriteIncr(ctx, f, conf); err != nil {
		return fmt.Errorf("set pdf info: write: %w", err)
	}
	return nil
}
