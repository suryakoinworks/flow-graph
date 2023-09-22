package flow

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/dominikbraun/graph"
	"github.com/dominikbraun/graph/draw"
	"github.com/labstack/echo/v4"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
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
		isParallelNode    bool
		action            action
		aggregateNode     *node
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
		key := strings.ReplaceAll(n.key, "-", " ")
		key = cases.Title(language.English).String(key)
		if n.isConditionalNode {
			g.AddVertex(key, graph.VertexAttribute("shape", "diamond"), graph.VertexAttribute("colorscheme", "ylorbr3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))

			continue
		}

		if n.isTrueNode {
			g.AddVertex(key, graph.VertexAttribute("shape", "rectangle"), graph.VertexAttribute("colorscheme", "greens3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))

			continue
		}

		if n.isFalseNode {
			g.AddVertex(key, graph.VertexAttribute("shape", "rectangle"), graph.VertexAttribute("colorscheme", "reds3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))

			continue
		}

		g.AddVertex(key, graph.VertexAttribute("shape", "rectangle"), graph.VertexAttribute("colorscheme", "blues3"), graph.VertexAttribute("style", "filled"), graph.VertexAttribute("color", "2"), graph.VertexAttribute("fillcolor", "1"))
	}

	for from, to := range w.nodes {
		from = strings.ReplaceAll(from, "-", " ")
		from = cases.Title(language.English).String(from)
		for k, v := range to {
			k = strings.ReplaceAll(k, "-", " ")
			k = cases.Title(language.English).String(k)
			if v.label != "" {
				g.AddEdge(from, k, graph.EdgeAttribute("label", v.label))

				continue
			}

			g.AddEdge(from, k)
		}
	}

	buffer := bytes.Buffer{}

	k := strings.ReplaceAll(w.key, "-", " ")
	k = cases.Title(language.English).String(k)

	err := draw.DOT(g, &buffer, draw.GraphAttribute("label", k), draw.GraphAttribute("bgcolor", "lightgrey"), draw.GraphAttribute("labelloc", "t"))

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
		return errors.New("use AddParallelEdge() to use parallel node")
	}

	from.next = append(from.next, to)
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

func (w *workflow) AddParallelEdge(from *node, aggregate *node, parallels ...*node) error {
	if !w.validateNode(from, aggregate) {
		return errors.New("one or more nodes are not registered, use AddNode() to register the node")
	}

	if !w.validateNode(parallels...) {
		return errors.New("one or more nodes are not registered, use AddNode() to register the node")
	}

	if err := w.isCircular(aggregate, from); err != nil {
		return err
	}

	w.assignRoot(from)

	w.cLock.Lock()
	from.isParallelNode = true
	for _, n := range parallels {
		if err := w.isCircular(n, from); err != nil {
			return err
		}

		if err := w.isCircular(aggregate, n); err != nil {
			return err
		}

		if len(w.nodes[from.key]) == 0 {
			w.nodes[from.key] = make(map[string]vertex)
		}

		w.nodes[from.key][n.key] = vertex{
			from: from,
			to:   n,
		}

		w.nodes[n.key] = map[string]vertex{
			aggregate.key: {
				from: n,
				to:   aggregate,
			},
		}

		w.destinations[n.key] = append(w.destinations[n.key], from)
		w.destinations[aggregate.key] = append(w.destinations[aggregate.key], n)
	}

	from.next = parallels
	from.aggregateNode = aggregate

	w.destinations[aggregate.key] = append(w.destinations[aggregate.key], from)
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

	if err := w.isCircular(condition, from); err != nil {
		return err
	}

	w.assignRoot(from)

	w.cLock.Lock()

	condition.isConditionalNode = true

	from.next = append(from.next, condition)

	trueNode.isTrueNode = true
	falseNode.isFalseNode = true

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

			if node.next[k].isParallelNode {
				result, err = w.executeParallel(node.next[k], result)
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

func (w *workflow) executeParallel(vertex *node, param []byte) ([]byte, error) {
	result := make(chan []byte)
	var err error

	res, err := vertex.action(map[string][]byte{"data": param})
	if err != nil {
		return nil, err
	}

	wg := sync.WaitGroup{}
	for _, n := range vertex.next {
		wg.Add(1)
		go func(n *node) {
			r, _ := n.action(map[string][]byte{"data": res})

			result <- r
		}(n)
	}

	rAggregate := make(map[string][]byte)
	for _, n := range vertex.next {
		rAggregate[n.key] = <-result
		wg.Done()
	}
	wg.Wait()
	close(result)

	rAggregate["data"] = res

	res, err = vertex.aggregateNode.action(rAggregate)
	if err != nil {
		return nil, err
	}

	if vertex.aggregateNode.next[0].isConditionalNode {
		return w.executeCondition(vertex.aggregateNode.next[0], res)
	}

	if vertex.aggregateNode.next[0].isParallelNode {
		return w.executeParallel(vertex.aggregateNode.next[0], res)
	}

	return w.execute(vertex.aggregateNode.next[0], res)
}

func (w *workflow) executeCondition(node *node, param []byte) ([]byte, error) {
	res, err := node.action(map[string][]byte{"data": param})
	if err != nil {
		return nil, err
	}

	status, _ := strconv.ParseBool(string(res))
	if status {
		if node.next[0].isParallelNode {
			return w.executeParallel(node.next[0], param)
		}

		if node.next[0].isConditionalNode {
			return w.executeCondition(node.next[0], param)
		}

		return w.execute(node.next[0], param)
	}

	if node.next[1].isParallelNode {
		return w.executeParallel(node.next[1], param)
	}

	if node.next[1].isConditionalNode {
		return w.executeCondition(node.next[1], param)
	}

	return w.execute(node.next[1], param)
}

func NewNode(key string, param action) *node {
	return &node{
		key:    key,
		action: param,
		next:   make([]*node, 0),
	}
}

func (n *node) Trigger(param map[string][]byte) ([]byte, error) {
	return n.action(param)
}
