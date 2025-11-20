package factory

import (
	"io/ioutil"
	"log"

	"gopkg.in/yaml.v2"
)

type SbiConfig struct {
	BindingIPv4 string `yaml:"bindingIPv4"`
	Port        int    `yaml:"port"`
}

type Mongodb struct {
	Name string `yaml:"name"`
	Url  string `yaml:"url"`
}


type NwdafConfig struct {
	Configuration struct {
		Sbi SbiConfig `yaml:"sbi"`
		Mongodb Mongodb `yaml:"mongodb"`
	} `yaml:"configuration"`
}

var NwdafConfigInstance = &NwdafConfig{}

func InitConfigFactory(configPath string) {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		log.Printf("[ERROR] Failed to read config file: %v", err)
		return
	}
	err = yaml.Unmarshal(data, NwdafConfigInstance)
	if err != nil {
		log.Printf("[ERROR] Failed to unmarshal config: %v", err)
		return
	}
	log.Printf("[INFO] Loaded config: %+v", NwdafConfigInstance)
}
