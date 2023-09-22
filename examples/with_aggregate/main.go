package main

import (
	"fmt"
	"log"
	"os"

	"github.com/ad3n/flow-graph"
)

func main() {
	node1 := flow.NewNode("get-input", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node1", param["data"])), nil
	})
	node2 := flow.NewNode("transform-user", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s node2", param["data"])), nil
	})
	node3 := flow.NewNode("validate-user", func(param map[string][]byte) ([]byte, error) {
		return []byte("true"), nil
	})
	node4 := flow.NewNode("save-user", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("save-user %s", param["data"])), nil
	})
	node5 := flow.NewNode("error-response", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("error-response %s", param["data"])), nil
	})
	node6 := flow.NewNode("send-sms", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s send-sms", param["data"])), nil
	})
	node7 := flow.NewNode("send-notification", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s send-notification", param["data"])), nil
	})
	node8 := flow.NewNode("send-email", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s send-email", param["data"])), nil
	})
	node9 := flow.NewNode("success-response", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("success-response [aggregate][%s, %s, %s] %s", param["send-sms"], param["send-notification"], param["send-email"], param["data"])), nil
	})
	node10 := flow.NewNode("send-response", func(param map[string][]byte) ([]byte, error) {
		return []byte(fmt.Sprintf("%s send-response", param["data"])), nil
	})

	workflow := flow.NewWorkflow("add-user")
	workflow.AddNode(node1, node2, node3, node4, node5, node6, node7, node8, node9, node10)
	if err := workflow.AddEdge(node1, node2); err != nil {
		log.Fatalln(err)
	}

	if err := workflow.AddConditionalEdge(node2, node3, node4, node5); err != nil {
		log.Fatalln(err)
	}

	if err := workflow.AddParallelEdge(node4, node9, node6, node7, node8); err != nil {
		log.Fatalln(err)
	}

	if err := workflow.AddEdge(node9, node10); err != nil {
		log.Fatalln(err)
	}

	if err := workflow.AddEdge(node5, node10); err != nil {
		log.Fatalln(err)
	}

	result, _ := workflow.Execute([]byte("hallo"))

	fmt.Println(string(result))

	dot, _ := workflow.Export()

	file, _ := os.Create(fmt.Sprintf("%s.gv", workflow.GetName()))
	file.Write(dot)
	file.Sync()
	file.Close()
}
