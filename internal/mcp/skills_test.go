package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

func skillsClient(t *testing.T) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()
	root, err := filepath.Abs("../../docs/samples/sales-dashboard")
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(Deps{State: newTestState(t, root)})
	ct, st := mcpsdk.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	if err := mcpsdk.AddSendingCustomMethod[*listSkillsParams, *listSkillsResult](client, "skills/list"); err != nil {
		t.Fatal(err)
	}
	if err := mcpsdk.AddSendingCustomMethod[*getSkillParams, *getSkillResult](client, "skills/get"); err != nil {
		t.Fatal(err)
	}
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// The server declares the Skills extension and serves every skill as
// skill:// resources whose bytes match the digests skills/list publishes.
func TestSkillsExtension(t *testing.T) {
	cs := skillsClient(t)
	ctx := context.Background()

	caps := cs.InitializeResult().Capabilities
	if caps.Resources == nil || caps.Extensions[SkillsExtension] == nil {
		t.Fatalf("capabilities lack resources or the skills extension: %+v", caps)
	}

	list, err := mcpsdk.CallCustomMethod[*listSkillsParams, *listSkillsResult](ctx, cs, "skills/list", &listSkillsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if list.ResultType != "complete" || list.CacheScope != "public" || list.TTLMs <= 0 || len(list.Skills) != len(mcpSkills) {
		t.Fatalf("skills/list = %+v", list)
	}
	sawReference := false
	for _, sk := range list.Skills {
		name, _ := sk.Frontmatter["name"].(string)
		if sk.URI != "skill://"+name+"/SKILL.md" || sk.Frontmatter["description"] == nil {
			t.Errorf("entry %s: uri or frontmatter wrong: %+v", sk.URI, sk.Frontmatter)
		}
		if len(sk.Resources) == 0 || sk.Resources[0].URI != sk.URI {
			t.Errorf("entry %s: resources do not start with SKILL.md", sk.URI)
		}
		for _, r := range sk.Resources {
			got, err := cs.ReadResource(ctx, &mcpsdk.ReadResourceParams{URI: r.URI})
			if err != nil {
				t.Fatalf("read %s: %v", r.URI, err)
			}
			sum := sha256.Sum256([]byte(got.Contents[0].Text))
			if "sha256:"+hex.EncodeToString(sum[:]) != r.Digest || len(got.Contents[0].Text) != r.Size {
				t.Errorf("%s: content does not match its digest or size", r.URI)
			}
			if !strings.HasPrefix(r.URI, strings.TrimSuffix(sk.URI, "SKILL.md")) {
				t.Errorf("%s is outside its skill", r.URI)
			}
			sawReference = sawReference || strings.Contains(r.URI, "/references/")
		}
	}
	if !sawReference {
		t.Error("no skill lists a references/ file")
	}

	got, err := mcpsdk.CallCustomMethod[*getSkillParams, *getSkillResult](ctx, cs, "skills/get", &getSkillParams{URI: "skill://bino-ibcs/SKILL.md"})
	if err != nil || got.Skill.URI != "skill://bino-ibcs/SKILL.md" || got.ResultType != "complete" || len(got.Skill.Resources) < 2 {
		t.Fatalf("skills/get bino-ibcs = %+v, %v", got, err)
	}
	_, err = mcpsdk.CallCustomMethod[*getSkillParams, *getSkillResult](ctx, cs, "skills/get", &getSkillParams{URI: "skill://bino-orchestration/SKILL.md"})
	var rpcErr *jsonrpc.Error
	if !errors.As(err, &rpcErr) || rpcErr.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("skills/get of a skill not served: %v, want -32602", err)
	}
}
