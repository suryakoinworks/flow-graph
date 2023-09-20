package main

import (
	"fmt"
	"log"

	"github.com/ad3n/flow-graph"
)

func main() {
	node1 := flow.NewNode("Get Input", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node1", param["data"])), nil
	})
	node2 := flow.NewNode("Transform to User", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node2", param["data"])), nil
	})
	node3 := flow.NewNode("Validate User", func(param map[string][]byte) ([]byte, error) {
		return []byte("false"), nil
	})
	node4 := flow.NewNode("Save User", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node4", param["data"])), nil
	})
	node5 := flow.NewNode("Error Response", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node5", param["data"])), nil
	})
	node6 := flow.NewNode("Success Response", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node6", param["data"])), nil
	})
	node7 := flow.NewNode("Send Response", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node7", param["data"])), nil
	})

	workflow := flow.NewWorkflow("Add User")
	workflow.AddNode(node1, node2, node3, node4, node5, node6, node7)
	if err := workflow.AddEdge(node1, node2); err != nil {
		log.Fatalln(err)
	}

	if err := workflow.AddConditionalEdge(node2, node3, node4, node5); err != nil {
		log.Fatalln(err)
	}

	if err := workflow.AddEdge(node4, node6); err != nil {
		log.Fatalln(err)
	}

	if err := workflow.AddEdge(node6, node7); err != nil {
		log.Fatalln(err)
	}

	if err := workflow.AddEdge(node5, node7); err != nil {
		log.Fatalln(err)
	}

	storage := flow.NewInMemoryStorage()
	storage.Save(workflow)

	w, err := storage.Get(workflow.GetName())
	if err != nil {
		log.Fatalln(err)
	}

	result, _ := w.Execute([]byte("from storage"))

	fmt.Println(string(result))
}
