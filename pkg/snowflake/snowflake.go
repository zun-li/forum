package snowflake

import bwsnowflake "github.com/bwmarrin/snowflake"

// Generator 可供多个请求并发使用；不同实例必须使用不同的节点编号。
type Generator struct {
	node *bwsnowflake.Node
}

func New(nodeID int64) (*Generator, error) {
	node, err := bwsnowflake.NewNode(nodeID)
	if err != nil {
		return nil, err
	}
	return &Generator{node: node}, nil
}

func (g *Generator) Next() uint64 {
	return uint64(g.node.Generate().Int64())
}
