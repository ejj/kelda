package inspect

import (
	"fmt"

	"github.com/kelda/kelda/blueprint"
)

// A Node in the communication Graph.
type Node struct {
	Name        string
	Connections map[string]Node
}

// An Edge in the communication Graph.
type Edge struct {
	From string
	To   string
}

// A Graph represents permission to communicate across a series of Nodes.
// Each Node is a container and each edge is permissions to
// initiate a connection.
type Graph struct {
	Nodes map[string]Node
}

// New queries the Blueprint to create a Graph structure.
func New(bp blueprint.Blueprint) (Graph, error) {
	g := Graph{
		Nodes: map[string]Node{},
	}

	for _, c := range bp.Containers {
		g.addNode(c.Hostname)
	}
	for _, lb := range bp.LoadBalancers {
		g.addNode(lb.Name)
	}
	g.addNode(blueprint.PublicInternetLabel)

	for _, conn := range bp.Connections {
		err := g.addConnection(conn.From, conn.To)
		if err != nil {
			return Graph{}, err
		}
	}

	return g, nil
}

// GetConnections returns a list of the edges in the Graph.
func (g Graph) GetConnections() []Edge {
	var res []Edge
	for _, n := range g.Nodes {
		for _, edge := range n.Connections {
			res = append(res, Edge{From: n.Name, To: edge.Name})
		}
	}
	return res
}

func (g *Graph) addConnection(fromSlice, toSlice []string) error {
	for _, from := range fromSlice {
		for _, to := range toSlice {
			fromNode, ok := g.Nodes[from]
			if !ok {
				return fmt.Errorf("no node: %s", from)
			}

			toNode, ok := g.Nodes[to]
			if !ok {
				return fmt.Errorf("no node: %s", to)
			}

			fromNode.Connections[to] = toNode
		}
	}
	return nil
}

func (g *Graph) addNode(hostname string) Node {
	n := Node{
		Name:        hostname,
		Connections: map[string]Node{},
	}
	g.Nodes[hostname] = n

	return n
}
