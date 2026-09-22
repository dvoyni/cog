package internal

import (
	"strings"
	"testing"

	"github.com/dvoyni/cog/bundles/model"
)

// Every member the shader declares is reachable by name, and the two that are
// not are named here on purpose.
//
// The names in member are transcribed from the WGSL by hand, and a hand
// transcription of an asset has been the wrong thing more than once in this
// package. So the shader is the authority: this reads the struct scene ships
// and fails on the member somebody adds without a way to address it, which is
// the whole reason the per-slot metadata is flat rather than an array.
func TestEveryPbrRecordMemberTheShaderDeclaresIsAddressable(t *testing.T) {
	// The struct lives in one of the nine sources scene.wgsl composes, so the
	// authority is the flattened module rather than any one file.
	source := flattenedSceneShader(t)
	// pad is not a member anyone means, and uvSets is a packed five-bit
	// selector no parameter kind expresses - which TEXCOORD set a slot samples
	// is the file's statement about its own mesh.
	unaddressable := map[string]bool{"uvSets": true, "pad": true}
	var record model.ScenePbrRecord
	members := wgslStructMembers(t, source, "ScenePbrMaterial")
	if len(members) != 19 {
		t.Fatalf("parsed %d members of ScenePbrMaterial: %v", len(members), members)
	}
	for _, name := range members {
		vec, scalar := record.Member(name)
		if unaddressable[name] {
			if vec != nil || scalar != nil {
				t.Errorf("%s is addressable, but is listed as one that is not", name)
			}
			continue
		}
		if vec == nil && scalar == nil {
			t.Errorf("the shader declares %s and nothing can override it", name)
		}
	}
}

// wgslStructMembers lists the member names one WGSL struct declares, in order.
// It is a line scan rather than a parser because the struct it reads is one
// scene owns and formats itself.
func wgslStructMembers(t testing.TB, source, name string) []string {
	t.Helper()
	start := strings.Index(source, "struct "+name+" {")
	if start < 0 {
		t.Fatalf("the shader declares no struct %s", name)
	}
	body := source[start:]
	body = body[:strings.Index(body, "\n};")]
	var members []string
	for _, line := range strings.Split(body, "\n")[1:] {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") {
			continue
		}
		if member, _, ok := strings.Cut(line, ":"); ok {
			members = append(members, strings.TrimSpace(member))
		}
	}
	return members
}
