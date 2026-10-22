package pdf

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// ResolveTOCLinks rewrites URI link annotations whose URI starts with prefix
// into internal links to the named destination given by the URI fragment.
//
// The TOC is printed in a separate Chrome job, where the heading targets do
// not exist, so Chrome would drop plain "#id" links. Printing them as absolute
// URLs keeps the link rectangles; after the merge with the content PDF the
// named destinations exist and the links can point at them. Links whose
// destination is missing lose their action instead of pointing at the
// placeholder URL. It returns the number of resolved links.
func ResolveTOCLinks(filePath, prefix string) (int, error) {
	ctx, err := api.ReadContextFile(filePath)
	if err != nil {
		return 0, fmt.Errorf("read pdf for toc links: %w", err)
	}

	dests := map[string]types.Array{}
	if node, ok := ctx.Names["Dests"]; ok && node != nil {
		collectDestArraysFromNameTree(ctx, node, dests)
	}
	for name, val := range ctx.Dests {
		if arr := destArray(ctx, val); arr != nil {
			dests[strings.TrimPrefix(name, "/")] = arr
		}
	}

	resolved, changed := 0, false
	for pageNr := 1; pageNr <= ctx.PageCount; pageNr++ {
		pageDict, _, _, err := ctx.PageDict(pageNr, false)
		if err != nil || pageDict == nil {
			continue
		}
		annots, err := ctx.DereferenceArray(pageDict["Annots"])
		if err != nil {
			continue
		}
		for _, obj := range annots {
			annot, err := ctx.DereferenceDict(obj)
			if err != nil || annot == nil || annot.NameEntry("Subtype") == nil || *annot.NameEntry("Subtype") != "Link" {
				continue
			}
			action, err := ctx.DereferenceDict(annot["A"])
			if err != nil || action == nil || action.NameEntry("S") == nil || *action.NameEntry("S") != "URI" {
				continue
			}
			uri, err := ctx.DereferenceStringOrHexLiteral(action["URI"], model.V10, nil)
			if err != nil || !strings.HasPrefix(uri, prefix) {
				continue
			}
			u, err := url.Parse(uri)
			if err != nil {
				continue
			}
			delete(annot, "A")
			changed = true
			if arr, ok := dests[u.Fragment]; ok {
				annot["Dest"] = arr
				resolved++
			}
		}
	}
	if !changed {
		return 0, nil
	}

	useXRefTable(ctx.Configuration)
	// WriteContextFile stages next to the target and keeps its file mode.
	if err := api.WriteContextFile(ctx, filePath); err != nil {
		return 0, fmt.Errorf("write pdf with toc links: %w", err)
	}
	return resolved, nil
}

func collectDestArraysFromNameTree(ctx *model.Context, node *model.Node, dests map[string]types.Array) {
	for name, val := range node.D {
		if arr := destArray(ctx, val); arr != nil {
			dests[name] = arr
		}
	}
	for _, child := range node.Kids {
		collectDestArraysFromNameTree(ctx, child, dests)
	}
}

// destArray returns the explicit destination array ([pageRef /XYZ ...]) of a
// named destination value, which is either the array or a dict with /D.
func destArray(ctx *model.Context, val types.Object) types.Array {
	val, _ = ctx.Dereference(val) //nolint:errcheck // nil on a broken reference means no destination
	if d, ok := val.(types.Dict); ok {
		val, _ = ctx.Dereference(d["D"]) //nolint:errcheck // nil on a broken reference means no destination
	}
	arr, ok := val.(types.Array)
	if !ok || len(arr) < 2 {
		return nil
	}
	if _, ok := arr[0].(types.IndirectRef); !ok {
		return nil
	}
	return arr
}
