package inspect

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/kelda/kelda/blueprint"
)

func TestStripExtension(t *testing.T) {
	test := map[string]string{
		"slug.blueprint":       "slug",
		"a/b/c/slug.blueprint": "a/b/c/slug",
		"foo":          "foo",
		"./foo/bar.js": "./foo/bar",
	}

	for inp, expect := range test {
		assert.Equal(t, expect, stripExtension(inp))
	}
}

// The expected graphviz graph returned by inspect when run on `bp` in TestViz.
const expGraph = `strict digraph {
    "a";
    "b";
    "c";
    "public";

    "a" -> "b";
    "b" -> "c";
}`

func isGraphEqual(a, b string) bool {
	a = strings.Replace(a, "\n", "", -1)
	a = strings.Replace(a, " ", "", -1)
	b = strings.Replace(b, "\n", "", -1)
	b = strings.Replace(b, " ", "", -1)
	return a == b
}

func TestViz(t *testing.T) {
	t.Parallel()

	bp := blueprint.Blueprint{
		Containers: []blueprint.Container{
			{
				Hostname: "a",
				ID:       "54be1283e837c6e40ac79709aca8cdb8ec5f31f5",
				Image:    blueprint.Image{Name: "ubuntu"},
			},
			{
				Hostname: "b",
				ID:       "3c1a5738512a43c3122608ab32dbf9f84a14e5f9",
				Image:    blueprint.Image{Name: "ubuntu"},
			},
			{
				Hostname: "c",
				ID:       "cb129f8a27df770b1dac70955c227a57bc5c4af6",
				Image:    blueprint.Image{Name: "ubuntu"},
			},
		},
		Connections: []blueprint.Connection{
			{From: []string{"a"}, To: []string{"b"},
				MinPort: 22, MaxPort: 22},
			{From: []string{"b"}, To: []string{"c"},
				MinPort: 22, MaxPort: 22},
		},
	}

	graph, err := New(bp)
	if err != nil {
		panic(err)
	}

	gv := makeGraphviz(graph)
	if !isGraphEqual(gv, expGraph) {
		t.Error(gv + "\n" + expGraph)
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	cmd := &Inspect{}
	assert.Error(t, cmd.Parse([]string{"test.js"}), "")
	assert.Error(t, cmd.Parse([]string{"test.js", "not_a_format"}), "")
}
