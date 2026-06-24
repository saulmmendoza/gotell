package models

import (
	"encoding/json"

	"github.com/jinzhu/gorm"
	"github.com/netlify/gotell/conf"
	"github.com/pkg/errors"
)

type Instance struct {
	ID   string `json:"id"`
	UUID string `json:"uuid,omitempty"`

	RawBaseConfig string              `json:"-" sql:"type:text"`
	BaseConfig    *conf.Configuration `json:"config" gorm:"-"`
}

func (i *Instance) TableName() string {
	return "instances"
}

func (i *Instance) AfterFind() error {
	if i.RawBaseConfig != "" {
		err := json.Unmarshal([]byte(i.RawBaseConfig), &i.BaseConfig)
		if err != nil {
			return err
		}
	}
	return nil
}

func (i *Instance) BeforeSave() error {
	if i.BaseConfig != nil {
		data, err := json.Marshal(i.BaseConfig)
		if err != nil {
			return err
		}
		i.RawBaseConfig = string(data)
	}
	return nil
}

func (i *Instance) Config() (*conf.Configuration, error) {
	if i.BaseConfig == nil {
		return nil, errors.New("no configuration data available")
	}

	baseConf := &conf.Configuration{}
	*baseConf = *i.BaseConfig

	return baseConf, nil
}

func GetInstance(db *gorm.DB, instanceID string) (*Instance, error) {
	instance := Instance{}
	if rsp := db.Where("id = ?", instanceID).First(&instance); rsp.Error != nil {
		return nil, errors.Wrap(rsp.Error, "error finding instance")
	}
	return &instance, nil
}

func GetInstanceByUUID(db *gorm.DB, uuid string) (*Instance, error) {
	instance := Instance{}
	if rsp := db.Where("uuid = ?", uuid).First(&instance); rsp.Error != nil {
		return nil, errors.Wrap(rsp.Error, "error finding instance")
	}
	return &instance, nil
}

func CreateInstance(db *gorm.DB, instance *Instance) error {
	if result := db.Create(instance); result.Error != nil {
		return errors.Wrap(result.Error, "Error creating instance")
	}
	return nil
}

func UpdateInstance(db *gorm.DB, instance *Instance) error {
	if result := db.Save(instance); result.Error != nil {
		return errors.Wrap(result.Error, "Error updating instance record")
	}
	return nil
}

func DeleteInstance(db *gorm.DB, instance *Instance) error {
	return db.Delete(instance).Error
}
