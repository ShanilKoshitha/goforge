package cli

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"strings"
	"testing"
	"text/template"
)

// Frozen digests cover every previously emitted resource template, including
// browser markup and generated tests. Normalize CRLF only for cross-platform
// checkouts; Go source is formatted exactly as the generator formats it.
// A maintainer can inspect a historical baseline with
// GOFORGE_CAPTURE_LEGACY=<commit> go test ./internal/cli -run
// TestResourceTemplatesRetainReleasedLegacySnapshots -v. Capture never writes
// goldens or accepts changed output in an ordinary test run.
var releasedResourceSnapshots = map[string]string{
	"8/legacy":   "89217d37af3a37d72a8b2b02261a0b0575b64050e4acd4e9800570f7091231f4",
	"8/typed":    "bf896ec7f3f7544a98929d07f0c8a758295fe3ff4415d54e7e226780c98950ac",
	"9/legacy":   "89217d37af3a37d72a8b2b02261a0b0575b64050e4acd4e9800570f7091231f4",
	"9/typed":    "bf896ec7f3f7544a98929d07f0c8a758295fe3ff4415d54e7e226780c98950ac",
	"10/legacy":  "18d13b746fea7b39c20297739e6433387fc7a2709b74dcef1f849f64626a9c77",
	"10/typed":   "d5ce5b3b2581a34346ffecbc9c94cfca684274d89aee40b05433f351556b1e23",
	"10/related": "a1808bbba4481a6284e7b68cb41dea68d649eab316ab95301812292604b3dd3f",
	"11/legacy":  "18d13b746fea7b39c20297739e6433387fc7a2709b74dcef1f849f64626a9c77",
	"11/typed":   "d5ce5b3b2581a34346ffecbc9c94cfca684274d89aee40b05433f351556b1e23",
	"11/related": "a1808bbba4481a6284e7b68cb41dea68d649eab316ab95301812292604b3dd3f",
	"12/legacy":  "18d13b746fea7b39c20297739e6433387fc7a2709b74dcef1f849f64626a9c77",
	"12/typed":   "d5ce5b3b2581a34346ffecbc9c94cfca684274d89aee40b05433f351556b1e23",
	"12/related": "a1808bbba4481a6284e7b68cb41dea68d649eab316ab95301812292604b3dd3f",
	"13/legacy":  "56e6421d210c77ad56cbb998fdb631ee8a904644490efc852456c77d3ceb9523",
	"13/typed":   "268f1a6c11f08c7c315f971bea41c5f22a0c4d2c84a8212e275bfd7488b06c78",
	"13/related": "3e296046fb7389df09f01296993f00586223e8f1b6dee59391fe63de47d7f0d6",
	"14/legacy":  "56e6421d210c77ad56cbb998fdb631ee8a904644490efc852456c77d3ceb9523",
	"14/typed":   "268f1a6c11f08c7c315f971bea41c5f22a0c4d2c84a8212e275bfd7488b06c78",
	"14/related": "3e296046fb7389df09f01296993f00586223e8f1b6dee59391fe63de47d7f0d6",
	"15/legacy":  "8582471939317b542aae96d7a0c728239c7f72e4b7ed598af9515705661b1014",
	"15/typed":   "198c7ca70b8279dc6e7caf105fa0cf277e41b318b9814425521b64b454db1c7b",
	"15/related": "81c63d0fa9416f92b77f402b4a1f839a4a518eea45a93f65c4a0b5c269f23bc4",
}

func TestResourceTemplatesRetainReleasedLegacySnapshots(t *testing.T) {
	reference := os.Getenv("GOFORGE_CAPTURE_LEGACY")
	referenceSources := make(map[string][]byte)
	for version := 8; version <= 15; version++ {
		for _, profile := range []string{"legacy", "typed", "related"} {
			if profile == "related" && version < 10 {
				continue
			}
			key := fmt.Sprintf("%d/%s", version, profile)
			t.Run(key, func(t *testing.T) {
				data := legacyResourceSnapshotData(t, version, profile)
				names := []string{"source_model.go", "model.go", "repository.go", "repository_test.go", "request.go", "controller.go", "controller_test.go", "web_controller.go", "web_controller_test.go", "up.sql",
					"views/index.forge.html", "views/form.forge.html", "views/new.forge.html", "views/edit.forge.html", "views/show.forge.html"}
				if version >= 13 {
					names = append(names, "authorization.go", "authorization_test.go")
				}
				digest := sha256.New()
				for _, name := range names {
					path := "templates/resource/" + name + ".tmpl"
					var source []byte
					var err error
					if reference == "" {
						source, err = templateFiles.ReadFile(path)
					} else {
						source = referenceSources[path]
						if source == nil {
							source, err = exec.Command("git", "show", reference+":internal/cli/"+path).Output()
							referenceSources[path] = source
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					parsed, err := template.New(name).Delims("[[", "]]").Option("missingkey=error").Parse(string(source))
					if err != nil {
						t.Fatal(err)
					}
					var output bytes.Buffer
					if err := parsed.Execute(&output, data); err != nil {
						t.Fatal(err)
					}
					contents := output.Bytes()
					if strings.HasSuffix(name, ".go") {
						contents, err = format.Source(contents)
						if err != nil {
							t.Fatalf("format %s: %v", name, err)
						}
					}
					contents = bytes.ReplaceAll(contents, []byte("\r\n"), []byte("\n"))
					if reference != "" {
						current, err := renderTemplate(path, name, data)
						if err != nil {
							t.Fatal(err)
						}
						actual := bytes.ReplaceAll([]byte(current), []byte("\r\n"), []byte("\n"))
						if !bytes.Equal(actual, contents) {
							first := 0
							for first < len(actual) && first < len(contents) && actual[first] == contents[first] {
								first++
							}
							start := max(0, first-30)
							t.Logf("DIFF %s %s at %d: current %q historical %q", key, name, first,
								actual[start:min(len(actual), first+65)], contents[start:min(len(contents), first+65)])
						}
					}
					fmt.Fprintf(digest, "%s\x00%s\x00", name, contents)
				}
				actual := fmt.Sprintf("%x", digest.Sum(nil))
				if reference != "" {
					t.Logf("%q: %q,", key, actual)
				} else if actual != releasedResourceSnapshots[key] {
					t.Errorf("released template output changed for %s: got %s; want %s", key, actual, releasedResourceSnapshots[key])
				}
			})
		}
	}
}

func legacyResourceSnapshotData(t *testing.T, version int, profile string) resourceTemplateData {
	t.Helper()
	fields := defaultResourceFields()
	if profile != "legacy" {
		var err error
		fields, err = parseResourceFields([]string{"title:string", "notes:text:nullable", "priority:integer", "estimate:integer:nullable", "active:boolean", "featured:boolean:nullable"})
		if err != nil {
			t.Fatal(err)
		}
	}
	data := resourceTemplateData{
		resourceSpec: resourceSpec{Name: "Ticket", Package: "ticket", Plural: "tickets", MigrationVersion: "20261021000000"},
		Module:       "example.com/snapshots", QueryAccessor: "Tickets", SchemaDriven: profile != "legacy",
		OwnerCandidateKey: version >= 10, OwnerKeyName: "tickets_owner_id_key", AuthorizationPolicies: version >= 13, AccessibleForms: version >= 15,
	}
	for _, field := range fields {
		projected := projectResourceTemplateField(field)
		data.Fields = append(data.Fields, projected)
		if projected.String || projected.Text {
			data.HasTextualFields = true
			if data.FirstTextualField == nil {
				data.FirstTextualField = &data.Fields[len(data.Fields)-1]
			}
		}
	}
	if profile == "related" {
		data.Relationships = []resourceTemplateRelationship{{
			Name: "category", GoName: "Category", Label: "Category", ForeignKey: "category_id", ForeignKeyGoName: "CategoryID",
			TargetSpec:          resourceSpec{Name: "Category", Package: "category", Plural: "categories", MigrationVersion: "20261020000000"},
			TargetQueryAccessor: "Categories", IndexName: "tickets_category_id_idx", ConstraintName: "tickets_category_owner_fkey",
		}}
	}
	return data
}
