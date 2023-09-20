package main

import (
	"fmt"

	"github.com/ad3n/flow-graph"
)

func main() {
	node1 := flow.NewNode("Get Input", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node1", param["data"])), nil
	})

	res, _ := node1.Trigger(map[string][]byte{"data": []byte("input to")})

	fmt.Println(string(res))
}
