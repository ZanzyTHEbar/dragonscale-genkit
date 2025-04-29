package executor

import (
	"fmt"
	"os"
	"strings"

	"github.com/ZanzyTHEbar/dragonscale-genkit"
	"gopkg.in/yaml.v3"
)

type DAGFile struct {
	Name        string    `yaml:"name"`
	Description string    `yaml:"description"`
	Tasks       []DAGTask `yaml:"tasks"`
}

type DAGTask struct {
	ID        string                 `yaml:"id"`
	Tool      string                 `yaml:"tool"`
	Args      map[string]interface{} `yaml:"args"`
	DependsOn []string               `yaml:"depends_on"`
}

// DAGFileLoader defines an interface for loading a DAGFile from a source (e.g., file, bytes, etc.).
type DAGFileLoader interface {
	Load(source string) (*DAGFile, error)
	Format() string // e.g., "yaml", "json"
}

// loaderRegistry holds registered DAGFileLoaders by format name.
var loaderRegistry = make(map[string]DAGFileLoader)

// RegisterDAGFileLoader registers a new DAGFileLoader for a given format.
func RegisterDAGFileLoader(loader DAGFileLoader) {
	loaderRegistry[loader.Format()] = loader
}

// GetDAGFileLoader retrieves a loader by format name (e.g., "yaml").
func GetDAGFileLoader(format string) (DAGFileLoader, bool) {
	loader, ok := loaderRegistry[format]
	return loader, ok
}

// YAMLLoader implements DAGFileLoader for YAML files.
type YAMLLoader struct{}

func (YAMLLoader) Load(path string) (*DAGFile, error) {
	return LoadDAGFile(path)
}

func (YAMLLoader) Format() string { return "yaml" }

func init() {
	RegisterDAGFileLoader(YAMLLoader{})
}

// LoadDAGFile parses a YAML DAG file and returns a DAGFile struct.
func LoadDAGFile(path string) (*DAGFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open DAG file: %w", err)
	}
	defer f.Close()
	var dag DAGFile
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&dag); err != nil {
		return nil, fmt.Errorf("failed to parse DAG YAML: %w", err)
	}
	return &dag, nil
}

// toArgumentSource converts a YAML argument value to a dragonscale.ArgumentSource.
func toArgumentSource(arg interface{}) dragonscale.ArgumentSource {
	if s, ok := arg.(string); ok && strings.HasPrefix(s, "$") {
		// Reference: $task_id.output or $task_id.output.field
		ref := strings.TrimPrefix(s, "$")
		parts := strings.Split(ref, ".")
		if len(parts) >= 2 && parts[1] == "output" {
			depTaskID := parts[0]
			outputField := ""
			if len(parts) > 2 {
				outputField = parts[2]
			}
			return dragonscale.ArgumentSource{
				Type:             dragonscale.ArgumentSourceDependencyOutput,
				DependencyTaskID: depTaskID,
				OutputFieldName:  outputField,
			}
		}
		// TODO: support more reference types
	}
	// Literal value
	return dragonscale.ArgumentSource{
		Type:  dragonscale.ArgumentSourceLiteral,
		Value: arg,
	}
}

// Validate checks the DAGFile for duplicate IDs, missing dependencies, and cycles.
func (dag *DAGFile) Validate() error {
	idSet := make(map[string]struct{}, len(dag.Tasks))
	for _, t := range dag.Tasks {
		if _, exists := idSet[t.ID]; exists {
			return fmt.Errorf("duplicate task ID found: %s", t.ID)
		}
		idSet[t.ID] = struct{}{}
	}
	// Check that all dependencies exist
	for _, t := range dag.Tasks {
		for _, dep := range t.DependsOn {
			if _, exists := idSet[dep]; !exists {
				return fmt.Errorf("task '%s' depends on missing task '%s'", t.ID, dep)
			}
		}
	}
	// Check for cycles using DFS
	visited := make(map[string]bool, len(dag.Tasks))
	stack := make(map[string]bool, len(dag.Tasks))
	var hasCycle func(id string) bool
	hasCycle = func(id string) bool {
		if stack[id] {
			return true // cycle detected
		}
		if visited[id] {
			return false
		}
		visited[id] = true
		stack[id] = true
		task := dag.getTaskByID(id)
		if task != nil {
			for _, dep := range task.DependsOn {
				if hasCycle(dep) {
					return true
				}
			}
		}
		stack[id] = false
		return false
	}
	for _, t := range dag.Tasks {
		if hasCycle(t.ID) {
			return fmt.Errorf("cycle detected in DAG at task '%s'", t.ID)
		}
	}
	return nil
}

// getTaskByID returns a pointer to the DAGTask with the given ID, or nil if not found.
func (dag *DAGFile) getTaskByID(id string) *DAGTask {
	for i := range dag.Tasks {
		if dag.Tasks[i].ID == id {
			return &dag.Tasks[i]
		}
	}
	return nil
}

// ToExecutionPlan converts a DAGFile to a dragonscale.ExecutionPlan.
func (dag *DAGFile) ToExecutionPlan() *dragonscale.ExecutionPlan {
	tasks := make([]dragonscale.Task, 0, len(dag.Tasks))
	for _, dagTask := range dag.Tasks {
		args := make(map[string]dragonscale.ArgumentSource)
		for k, v := range dagTask.Args {
			args[k] = toArgumentSource(v)
		}
		tasks = append(tasks, dragonscale.Task{
			ID:        dagTask.ID,
			ToolName:  dagTask.Tool,
			Args:      args,
			DependsOn: dagTask.DependsOn,
		})
	}
	return dragonscale.NewExecutionPlan(tasks)
}

// LoadAndValidateDAG loads a DAG file using the default loader (YAML), validates it, and returns an ExecutionPlan.
func LoadAndValidateDAG(path string) (*dragonscale.ExecutionPlan, error) {
	loader, ok := GetDAGFileLoader("yaml")
	if !ok {
		return nil, fmt.Errorf("no YAML DAG loader registered")
	}

	dagFile, err := loader.Load(path)
	if err != nil {
		return nil, err
	}
	if err := dagFile.Validate(); err != nil {
		return nil, err
	}
	return dagFile.ToExecutionPlan(), nil
}
