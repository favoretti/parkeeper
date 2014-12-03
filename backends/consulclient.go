package backends

import (
	"strings"

	"github.com/armon/consul-api"
)

type ConsulClient struct {
	addr   string
	client *consulapi.Client
	kv     *consulapi.KV
}

func NewConsulClient(addr string) (*ConsulClient, error) {
	var c *consulapi.Client

	// configure address (and scheme if necessary)
	cfg := consulapi.DefaultConfig()
	cfg.Address = addr

	// create custom client
	c, err := consulapi.NewClient(cfg)
	if (err != nil) {
		return nil, err
	}

	return &ConsulClient { addr: addr, client: c, kv: c.KV() }, nil
}

func (c *ConsulClient) Create(path string, data string) *Error {
	return c.cas(path, data, uint64(0))
}

func (c *ConsulClient) Delete(path string, version int32) *Error {
	// cas? for DELETE: https://github.com/hashicorp/consul/issues/348
	_, err := c.kv.Delete(path, nil)
	if (err != nil) {
		return &Error { errCode: BackendUnreachable }
	}

	return nil
}

func (c *ConsulClient) Exists(path string) *Error {
	_, _, err := c.get(path, nil)
	if (err != nil) {
		return err
	}

	return nil
}

func (c *ConsulClient) GetData(path string) (*Node, *Error) {
	kv, _, err := c.get(path, nil)
	if (err != nil) {
		return nil, err
	}

	return mapFromKV(kv), nil
}

func (c *ConsulClient) SetData(path string, data string, version int32) *Error {
	kv, _, err := c.get(keyFromPath(path), nil)
	if (err != nil) {
		return err
	}

	//log.Debug(fmt.Sprintf("SetData: version: %d", version))

	modifyIndex := uint64(version)
	if (version == -1) {
		modifyIndex = kv.ModifyIndex
	}

	return c.cas(path, data, modifyIndex)
}

func (c *ConsulClient) GetChildren(path string) ([]string, *Error) {
	keyPath := keyFromPath(path) + "/"
	kvs, _, err := c.kv.Keys(keyPath, "/", nil)
	if (err != nil) {
		return nil, &Error { errCode: BackendUnreachable }
	}

	// clean up keys
	// * remove duplicates due to value and directories ("subdir/" & "subdir")
	// * remove subdir prefixes

	var childKey string
	childrenMap := make(map[string]bool)
	for _, kv := range kvs {
		childKey = kv
		childKey = strings.TrimPrefix(childKey, keyPath)
		childKey = strings.TrimSuffix(childKey, "/")
		if (childKey != "") {
			childrenMap[childKey] = true
		}
	}

	children := make([]string, 0, len(childrenMap))
	for key := range childrenMap {
		children = append(children, key)
	}

	return children, nil
}

func (c *ConsulClient) get(path string, q *consulapi.QueryOptions) (*consulapi.KVPair, *consulapi.QueryMeta, *Error) {
	kv, qm, err := c.kv.Get(keyFromPath(path), nil)
	if (err != nil) {
		return nil, qm, &Error { errCode: BackendUnreachable }
	}

	if (kv == nil) {
		return nil, qm, &Error { errCode: KeyNotFound }
	}

	return kv, qm, nil
}

func (c *ConsulClient) cas(path, data string, modifyIndex uint64) *Error {
	kv := &consulapi.KVPair {
		Key: keyFromPath(path),
		Value: []byte(data),
		ModifyIndex: modifyIndex,
	}

	wasOk, _, err := c.kv.CAS(kv, nil)
	if (err != nil) {
		//log.Debug(err.Error())
		return &Error { errCode: BackendUnreachable }
	}

	if (!wasOk) {
		if (modifyIndex == 0) {
			return &Error { errCode: BadVersion }
		} else {
			return &Error { errCode: KeyExists }
		}
	}

	return nil
}

func keyFromPath(path string) string {
	return strings.TrimPrefix(path, "/")
}

func mapFromKV(kv *consulapi.KVPair) *Node {
	return &Node {
		Path:          kv.Key,
		Value:         string(kv.Value),
		CreatedIndex:  kv.CreateIndex,
		ModifiedIndex: kv.ModifyIndex,
	}
}