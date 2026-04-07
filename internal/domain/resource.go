package domain

type CloudProvider string

const (
	CloudAWS CloudProvider = "aws"
)

type Resources struct {
	Cloud        CloudProvider `yaml:"cloud,omitempty" json:"cloud,omitempty"`
	Region       string        `yaml:"region,omitempty" json:"region,omitempty"`
	Zone         string        `yaml:"zone,omitempty" json:"zone,omitempty"`
	Accelerators string        `yaml:"accelerators,omitempty" json:"accelerators,omitempty"`
	CPUs         string        `yaml:"cpus,omitempty" json:"cpus,omitempty"`
	Memory       string        `yaml:"memory,omitempty" json:"memory,omitempty"`
	InstanceType string        `yaml:"instance_type,omitempty" json:"instance_type,omitempty"`
	UseSpot      bool          `yaml:"use_spot,omitempty" json:"use_spot,omitempty"`
	DiskSizeGB   int           `yaml:"disk_size,omitempty" json:"disk_size,omitempty"`
	Ports        []string      `yaml:"ports,omitempty" json:"ports,omitempty"`
	ImageID      string        `yaml:"image_id,omitempty" json:"image_id,omitempty"`
}

func (r Resources) String() string {
	if r.Accelerators != "" {
		return r.Accelerators
	}
	if r.InstanceType != "" {
		return r.InstanceType
	}
	return "-"
}
