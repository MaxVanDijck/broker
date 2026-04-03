package domain

type TaskSpec struct {
	Name      string            `yaml:"name,omitempty" json:"name,omitempty"`
	Workdir   string            `yaml:"workdir,omitempty" json:"workdir,omitempty"`
	NumNodes  int               `yaml:"num_nodes,omitempty" json:"num_nodes,omitempty"`
	Resources *Resources        `yaml:"resources,omitempty" json:"resources,omitempty"`
	Envs      map[string]string `yaml:"envs,omitempty" json:"envs,omitempty"`
	Setup     string            `yaml:"setup,omitempty" json:"setup,omitempty"`
	Run       string            `yaml:"run,omitempty" json:"run,omitempty"`
}

func (t *TaskSpec) ApplyDefaults() {
	if t.NumNodes <= 0 {
		t.NumNodes = 1
	}
	if t.Resources == nil {
		t.Resources = &Resources{}
	}
}
