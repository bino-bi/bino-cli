package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"path"
	"slices"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	claudeplugin "bino.bi/bino/claude-plugin"
)

// SkillsExtension is the MCP Skills extension (SEP-2640): skills as skill://
// resources, listed by skills/list and confirmed by skills/get.
const SkillsExtension = "io.modelcontextprotocol/skills"

// skillsTTLMs is the cache hint on skill results. The skills are part of the
// binary, so they only change with a new bino.
const skillsTTLMs = 3_600_000

// SkillFile is one file of a served skill.
type SkillFile struct {
	URI      string `json:"uri"`
	Path     string `json:"path"`
	MIMEType string `json:"mimeType"`
	Digest   string `json:"digest"`
	Size     int    `json:"size"`
	Text     string `json:"text"`
}

// SkillEntry is one served skill: its SKILL.md URI, its frontmatter verbatim
// and every file.
type SkillEntry struct {
	URI         string         `json:"uri"`
	Frontmatter map[string]any `json:"frontmatter"`
	Files       []SkillFile    `json:"files"`
}

var (
	catalogOnce sync.Once
	catalog     []SkillEntry
	errCatalog  error
)

// SkillCatalog returns the skills this server serves (mcpSkills), built once
// from the embedded plugin skills.
func SkillCatalog() ([]SkillEntry, error) {
	catalogOnce.Do(func() {
		dirs, err := claudeplugin.SkillDirs()
		if err != nil {
			errCatalog = err
			return
		}
		for _, d := range dirs {
			if !slices.Contains(mcpSkills, d.Name) {
				continue
			}
			e := SkillEntry{URI: skillURI(d.Name, "SKILL.md"), Frontmatter: d.Frontmatter}
			for _, f := range d.Files {
				sum := sha256.Sum256(f.Data)
				e.Files = append(e.Files, SkillFile{
					URI: skillURI(d.Name, f.Path), Path: f.Path, MIMEType: skillMIME(f.Path),
					Digest: "sha256:" + hex.EncodeToString(sum[:]), Size: len(f.Data), Text: string(f.Data),
				})
			}
			catalog = append(catalog, e)
		}
		if len(catalog) != len(mcpSkills) {
			errCatalog = fmt.Errorf("mcp: %d of %d skills embedded", len(catalog), len(mcpSkills))
		}
	})
	return catalog, errCatalog
}

func skillURI(name, file string) string { return "skill://" + name + "/" + file }

func skillMIME(p string) string {
	if path.Ext(p) == ".md" {
		return "text/markdown"
	}
	if t := mime.TypeByExtension(path.Ext(p)); t != "" {
		return t
	}
	return "text/plain"
}

// Wire types of skills/list and skills/get.
type skillResource struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

type wireSkill struct {
	URI         string          `json:"uri"`
	Frontmatter map[string]any  `json:"frontmatter"`
	Resources   []skillResource `json:"resources"`
}

type listSkillsParams struct {
	mcpsdk.ParamsBase
	Cursor string `json:"cursor,omitempty"`
}

type listSkillsResult struct {
	mcpsdk.ResultBase
	ResultType string `json:"resultType"`
	mcpsdk.Cacheable
	Skills []wireSkill `json:"skills"`
}

type getSkillParams struct {
	mcpsdk.ParamsBase
	URI string `json:"uri"`
}

type getSkillResult struct {
	mcpsdk.ResultBase
	ResultType string `json:"resultType"`
	mcpsdk.Cacheable
	Skill wireSkill `json:"skill"`
}

func toWire(e SkillEntry) wireSkill {
	w := wireSkill{URI: e.URI, Frontmatter: e.Frontmatter, Resources: make([]skillResource, 0, len(e.Files))}
	for _, f := range e.Files {
		w.Resources = append(w.Resources, skillResource{URI: f.URI, Digest: f.Digest, Size: f.Size})
	}
	return w
}

// SkillsCapabilities are the server capabilities that declare the Skills
// extension. They replace the SDK's default, which only declares logging, a
// feature the 2026-07-28 revision deprecates and bino does not use.
func SkillsCapabilities() *mcpsdk.ServerCapabilities {
	return &mcpsdk.ServerCapabilities{
		Extensions: map[string]any{SkillsExtension: map[string]any{}},
	}
}

// RegisterSkills serves entries as the Skills extension on srv: each file as
// a skill:// resource, plus skills/list and skills/get. srv must declare the
// extension (SkillsCapabilities). Another server that serves the same skills,
// such as a hosted gateway, uses it too.
func RegisterSkills(srv *mcpsdk.Server, entries []SkillEntry) error {
	byURI := make(map[string]SkillEntry, len(entries))
	for _, e := range entries {
		byURI[e.URI] = e
		for _, f := range e.Files {
			r := &mcpsdk.Resource{URI: f.URI, Name: f.Path, MIMEType: f.MIMEType, Size: int64(f.Size)}
			if f.Path == "SKILL.md" {
				r.Name, _ = e.Frontmatter["name"].(string)
				r.Description, _ = e.Frontmatter["description"].(string)
			}
			contents := &mcpsdk.ResourceContents{URI: f.URI, MIMEType: f.MIMEType, Text: f.Text}
			srv.AddResource(r, func(context.Context, *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
				return &mcpsdk.ReadResourceResult{Contents: []*mcpsdk.ResourceContents{contents}}, nil
			})
		}
	}
	cache := mcpsdk.Cacheable{TTLMs: skillsTTLMs, CacheScope: "public"}
	if err := mcpsdk.AddReceivingCustomMethod(srv, "skills/list",
		func(context.Context, *mcpsdk.ServerSession, *listSkillsParams) (*listSkillsResult, error) {
			res := &listSkillsResult{ResultType: "complete", Cacheable: cache, Skills: make([]wireSkill, 0, len(entries))}
			for _, e := range entries {
				res.Skills = append(res.Skills, toWire(e))
			}
			return res, nil
		}); err != nil {
		return err
	}
	return mcpsdk.AddReceivingCustomMethod(srv, "skills/get",
		func(_ context.Context, _ *mcpsdk.ServerSession, p *getSkillParams) (*getSkillResult, error) {
			e, ok := byURI[p.URI]
			if !ok {
				return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("no skill at %q", p.URI)}
			}
			return &getSkillResult{ResultType: "complete", Cacheable: cache, Skill: toWire(e)}, nil
		})
}
