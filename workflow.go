package flow

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/labstack/echo/v4"
)

type (
	action func(param map[string][]byte) ([]byte, error)

	Storage interface {
		Save(workflow *workflow) error
		Get(name string) (*workflow, error)
	}

	server struct {
		storage Storage
		server  *echo.Echo
	}

	inMemoryStorage struct {
		workflows map[string]*workflow
	}

	node struct {
		key               string
		isTrueNode        bool
		isFalseNode       bool
		isConditionalNode bool
		action            action
		prev              []*node
		next              []*node
	}

	workflow struct {
		key            string
		cLock          *sync.Mutex
		root           *node
		availableNodes map[string]*node
		nodes          map[string]map[string]vertex
		destinations   map[string][]*node
	}

	vertex struct {
		from  *node
		to    *node
		label string
	}

	Execute struct {
		Param string `json:"param" form:"param"`
	}
)

func NewServer(storage Storage) *server {
	e := echo.New()
	e.POST("/execute/:workflow", func(c echo.Context) error {
		workflow := Execute{}
		if err := c.Bind(&workflow); err != nil {
			return c.JSON(http.StatusBadRequest, map[string]string{
				"message": "invalid request.",
			})
		}

		w, err := storage.Get(c.Param("workflow"))
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{
				"message": err.Error(),
			})
		}

		res, err := w.Execute([]byte(workflow.Param))
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"message": err.Error(),
			})
		}

		return c.JSON(http.StatusOK, map[string]string{
			"result": string(res),
		})
	})

	e.GET("/export/:workflow", func(c echo.Context) error {
		w, err := storage.Get(c.Param("workflow"))
		if err != nil {
			return c.JSON(http.StatusNotFound, map[string]string{
				"message": err.Error(),
			})
		}

		res, err := w.Export()
		if err != nil {
			return c.JSON(http.StatusInternalServerError, map[string]string{
				"message": err.Error(),
			})
		}

		return c.JSON(http.StatusOK, map[string]string{
			"dot": string(res),
		})
	})

	return &server{
		storage: storage,
		server:  e,
	}
}

func (s *server) Start(port int) error {
	return s.server.Start(fmt.Sprintf(":%d", port))
}

func (s *server) GetEcho() *echo.Echo {
	return s.server
}

func NewInMemoryStorage() *inMemoryStorage {
	return &inMemoryStorage{
		workflows: make(map[string]*workflow),
	}
}

func (s *inMemoryStorage) Save(workflow *workflow) error {
	s.workflows[workflow.key] = workflow

	return nil
}

func (s *inMemoryStorage) Get(name string) (*workflow, error) {
	w, ok := s.workflows[name]
	if !ok {
		return nil, fmt.Errorf("workflow '%s' not found", name)
	}

	return w, nil
}

func NewWorkflow(name string) *workflow {
	return &workflow{
		key:            name,
		availableNodes: make(map[string]*node),
		nodes:          make(map[string]map[string]vertex),
		destinations:   make(map[string][]*node),
		cLock:          &sync.Mutex{},
	}
}

func (w *workflow) Export() ([]byte, error) {
	g := graph.New(graph.StringHash, graph.Directed(), graph.Acyclic())
	for _, n := range w.availableNodes {
		if n.isConditionalNode {
			g.AddVertex(n.key, graph.VertexAttribute("shape", "diamond"), graph.VertexAttribute("colorscheme", "ylorbr3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))

			continue
		}

		if n.isTrueNode {
			g.AddVertex(n.key, graph.VertexAttribute("shape", "rectangle"), graph.VertexAttribute("colorscheme", "greens3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))

			continue
		}

		if n.isFalseNode {
			g.AddVertex(n.key, graph.VertexAttribute("shape", "rectangle"), graph.VertexAttribute("colorscheme", "reds3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))

			continue
		}

		g.AddVertex(n.key, graph.VertexAttribute("shape", "rectangle"), graph.VertexAttribute("colorscheme", "blues3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))
	}

	for from, to := range w.nodes {
		for k, v := range to {
			if v.label != "" {
				g.AddEdge(from, k, graph.EdgeAttribute("label", v.label))

				continue
			}

			g.AddEdge(from, k)
		}
	}

	buffer := bytes.Buffer{}

	err := draw.DOT(g, &buffer, draw.GraphAttribute("label", w.key), draw.GraphAttribute("bgcolor", "lightgrey"), draw.GraphAttribute("labelloc", "t"))

	return buffer.Bytes(), err
}

func (w *workflow) Execute(param []byte) ([]byte, error) {
	return w.execute(w.root, param)
}

func (w *workflow) AddNode(nodes ...*node) {
	for _, n := range nodes {
		w.availableNodes[n.key] = n
	}
}

func (w *workflow) GetRoot() *node {
	return w.root
}

func (w *workflow) GetName() string {
	return w.key
}

func (w *workflow) AddEdge(from *node, to *node) error {
	if !w.validateNode(from, to) {
		return errors.New("one or more nodes are not registered, use AddNode() to register the node")
	}

	if err := w.isCircular(to, from); err != nil {
		return err
	}

	if from.key == to.key {
		return errors.New("circular reference detected")
	}

	w.assignRoot(from)

	w.cLock.Lock()
	_, exists := w.nodes[from.key]
	if exists {
		return errors.New("parallel node is not supported yet")
	}

	from.next = append(from.next, to)
	to.prev = append(to.prev, from)

	w.nodes[from.key] = map[string]vertex{
		to.key: {
			from: from,
			to:   to,
		},
	}
	w.destinations[to.key] = append(w.destinations[to.key], from)
	w.cLock.Unlock()

	return nil
}

func (w *workflow) AddConditionalEdge(from *node, condition *node, trueNode *node, falseNode *node) error {
	if !w.validateNode(from, condition, trueNode, falseNode) {
		return errors.New("one or more nodes are not registered, use AddNode() to register the node")
	}

	if err := w.isCircular(trueNode, from); err != nil {
		return err
	}

	if err := w.isCircular(falseNode, from); err != nil {
		return err
	}

	w.assignRoot(from)

	w.cLock.Lock()
	if from.key == trueNode.key || from.key == falseNode.key {
		return errors.New("circular reference detected")
	}

	condition.isConditionalNode = true

	from.next = append(from.next, condition)
	condition.prev = append(condition.prev, from)

	trueNode.isTrueNode = true
	falseNode.isFalseNode = true

	trueNode.prev = append(trueNode.prev, condition)
	falseNode.prev = append(falseNode.prev, condition)

	condition.next = []*node{trueNode, falseNode}

	w.nodes[from.key] = map[string]vertex{
		condition.key: {
			from: from,
			to:   condition,
		},
	}

	w.nodes[condition.key] = map[string]vertex{
		trueNode.key: {
			from:  condition,
			to:    trueNode,
			label: "true",
		},
	}
	w.destinations[trueNode.key] = append(w.destinations[trueNode.key], from)

	w.nodes[condition.key][falseNode.key] = vertex{
		from:  condition,
		to:    falseNode,
		label: "false",
	}
	w.destinations[falseNode.key] = append(w.destinations[falseNode.key], from)
	w.cLock.Unlock()

	return nil
}

func (w *workflow) assignRoot(node *node) {
	if w.root == nil {
		w.root = node
	}
}

func (w *workflow) validateNode(nodes ...*node) bool {
	for _, n := range nodes {
		_, ok := w.availableNodes[n.key]
		if !ok {
			return ok
		}
	}

	return true
}

func (w *workflow) isCircular(to *node, from *node) error {
	if w.root != nil && to.key == w.root.key {
		return fmt.Errorf("circular detection from '%s' to '%s'", from.key, w.root.key)
	}

	froms, ok := w.destinations[to.key]
	if !ok {
		return nil
	}

	for _, n := range froms {
		if n.key == from.key {
			return fmt.Errorf("circular detection from '%s' to '%s'", from.key, to.key)
		}
	}

	return nil
}

func (w *workflow) execute(node *node, param []byte) ([]byte, error) {
	var result []byte
	var err error

	result, err = node.action(map[string][]byte{"data": param})
	if len(node.next) > 0 {
		for k := 0; k < len(node.next); k++ {
			if node.next[k].isConditionalNode {
				result, err = w.executeCondition(node.next[k], result)
				if err != nil {
					return nil, err
				}

				continue
			}

			result, err = w.execute(node.next[k], result)
			if err != nil {
				return nil, err
			}
		}
	}

	return result, err
}

func (w *workflow) executeCondition(node *node, param []byte) ([]byte, error) {
	res, err := node.action(map[string][]byte{"data": param})
	if err != nil {
		return nil, err
	}

	status, _ := strconv.ParseBool(string(res))
	if status {
		return w.execute(node.next[0], param)
	}

	return w.execute(node.next[1], param)
}

func NewNode(key string, param action) *node {
	return &node{
		key:    key,
		action: param,
		prev:   make([]*node, 0),
		next:   make([]*node, 0),
	}
}

func (n *node) Trigger(param map[string][]byte) ([]byte, error) {
	return n.action(param)
}
