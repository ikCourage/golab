package gotrans

import (
	"fmt"
	"reflect"
	"sync"
	"unsafe"
)

type tField struct {
	FromKey   string
	ToKey     string
	Index     int
	IndexNode int
	IndexArr  []int
	Type      reflect.Type
	Kind      reflect.Kind
}

type typeDesc struct {
	Write map[string]*tField
	Read  []tField
}

type TransFunc func(from, to, node, root unsafe.Pointer) error

var (
	globalM = struct {
		rwLK         sync.RWMutex
		descMap      map[reflect.Type]*typeDesc
		transFuncMap map[string]TransFunc
	}{
		descMap:      make(map[reflect.Type]*typeDesc),
		transFuncMap: make(map[string]TransFunc),
	}
)

func getKeyByField(fd *reflect.StructField) (string, string, string) {
	key := fd.Name
	if 'A' <= key[0] && key[0] <= 'Z' {
		if tag, ok := fd.Tag.Lookup("trans2"); ok {
			l := len(tag)
			for i := 0; i < l; i++ {
				if tag[i] == ':' {
					return "", tag[:i], tag[i+1:]
				}
			}
			return "", "-", ""
		}
		if tag, ok := fd.Tag.Lookup("trans"); ok {
			if tag != "" {
				return tag, "", ""
			}
			return key, "", ""
		}
	}
	return "", "", ""
}

func isInvalidType(tt reflect.Type) (bool, error) {
__loop:
	switch tt.Kind() {
	case reflect.Struct:
		if _, err := getTypeDesc(tt); nil != err {
			return false, err
		}
	case reflect.Map, reflect.Slice, reflect.Array, reflect.Ptr:
		tt = tt.Elem()
		goto __loop
	}
	return false, nil
}

func (tdesc *typeDesc) walkStruct(rt reflect.Type) (err error) {
	count := 0
	l := rt.NumField()
	tdesc.Read = make([]tField, 0, l)
	tdesc.Write = make(map[string]*tField, l>>1)
	var tdesc2 *typeDesc
	var tfd *tField
	var key string
	var fromKey string
	var toKey string
	var invalid bool
	var fd reflect.StructField
	var kind reflect.Kind
	for i := 0; i < l; i++ {
		fd = rt.Field(i)
		if fd.Anonymous {
			tdesc2, err = getTypeDesc(fd.Type)
			if nil != err {
				return err
			}
			arr2 := tdesc2.Read
			l2 := len(arr2)
			if l2 != 0 {
				indexArr2 := []int{i}
				for i2 := 0; i2 < l2; i2++ {
					tdesc.Read = append(tdesc.Read, arr2[i2])
					tfd = &tdesc.Read[count]
					count++
					if nil != tfd.IndexArr {
						tfd.IndexArr = append(indexArr2, tfd.IndexArr...)
					} else {
						tfd.IndexArr = append(indexArr2, tfd.Index)
						tfd.IndexNode = i
					}
				}
				for k, v := range tdesc2.Write {
					if _, ok := tdesc.Write[k]; ok {
						return fmt.Errorf("duplicate key: %s at %v", k, v.Type)
					}
					tfd = &tField{
						Index: v.Index,
						Type:  v.Type,
					}
					if nil != v.IndexArr {
						tfd.IndexArr = append(indexArr2, v.IndexArr...)
					} else {
						tfd.IndexArr = append(indexArr2, v.Index)
					}
					tdesc.Write[k] = tfd
				}
			}
		} else {
			key, fromKey, toKey = getKeyByField(&fd)
			if fromKey != "" {
				kind = fd.Type.Kind()
				switch kind {
				case reflect.Struct:
					_, err = getTypeDesc(fd.Type)
					if nil != err {
						return
					}
				case reflect.Map, reflect.Slice, reflect.Array, reflect.Ptr:
					invalid, err = isInvalidType(fd.Type.Elem())
					if nil != err {
						return
					}
					if invalid {
						continue
					}
				case reflect.Func, reflect.Chan, reflect.UnsafePointer:
					continue
				}
				tdesc.Read = append(tdesc.Read, tField{
					FromKey: fromKey,
					ToKey:   toKey,
					Index:   i,
					Type:    fd.Type,
					Kind:    kind,
				})
				count++
			} else if key != "" {
				if _, ok := tdesc.Write[key]; ok {
					return fmt.Errorf("duplicate key: %s at %v", key, rt)
				}
				tdesc.Write[key] = &tField{
					Index: i,
					Type:  fd.Type,
				}
			}
		}
	}
	for count > 0 {
		count--
		tfd = &tdesc.Read[count]
		if tfd.ToKey != "" {
			if _, ok := getTransFunc(tfd.FromKey); !ok {
				return fmt.Errorf("function %s not found, at %v", tfd.FromKey, tfd.Type)
			}
			if _, ok := tdesc.Write[tfd.ToKey]; !ok {
				return fmt.Errorf("field %s not found, at %v", tfd.ToKey, tfd.Type)
			}
		}
	}
	return
}

func getTypeDesc(rt reflect.Type) (*typeDesc, error) {
	globalM.rwLK.RLock()
	tdesc, ok := globalM.descMap[rt]
	globalM.rwLK.RUnlock()
	if ok {
		return tdesc, nil
	}
	tdesc = &typeDesc{}
	if err := tdesc.walkStruct(rt); nil != err {
		return nil, err
	}
	globalM.rwLK.Lock()
	globalM.descMap[rt] = tdesc
	globalM.rwLK.Unlock()
	return tdesc, nil
}

func getTransFunc(name string) (TransFunc, bool) {
	globalM.rwLK.RLock()
	f, ok := globalM.transFuncMap[name]
	globalM.rwLK.RUnlock()
	return f, ok
}

func RegisterTransFunc(name string, f TransFunc) {
	globalM.rwLK.Lock()
	if _, ok := globalM.transFuncMap[name]; ok {
		globalM.rwLK.Unlock()
		panic(fmt.Errorf("duplicate name %s", name))
	}
	globalM.transFuncMap[name] = f
	globalM.rwLK.Unlock()
}

func transValue(rv reflect.Value, rt reflect.Type, rk reflect.Kind, root unsafe.Pointer) (err error) {
	switch rk {
	case reflect.Struct:
		tdesc, err := getTypeDesc(rt)
		if nil != err {
			return err
		}
		var tfd *tField
		var vv, vv2 reflect.Value
		var node unsafe.Pointer
		nodeV := unsafe.Pointer(rv.UnsafeAddr())
		l := len(tdesc.Read)
		for i := 0; i < l; i++ {
			tfd = &tdesc.Read[i]
			if tfd.FromKey != "" {
				if nil != tfd.IndexArr {
					node = unsafe.Pointer(rv.Field(tfd.IndexNode).UnsafeAddr())
					vv = rv.FieldByIndex(tfd.IndexArr)
				} else {
					node = nodeV
					vv = rv.Field(tfd.Index)
				}
				if tfd.ToKey != "" {
					tfunc, _ := getTransFunc(tfd.FromKey)
					tfd = tdesc.Write[tfd.ToKey]
					if nil != tfd.IndexArr {
						vv2 = rv.FieldByIndex(tfd.IndexArr)
					} else {
						vv2 = rv.Field(tfd.Index)
					}
					err = tfunc(unsafe.Pointer(vv.UnsafeAddr()), unsafe.Pointer(vv2.UnsafeAddr()), node, root)
				} else {
					err = transValue(vv, tfd.Type, tfd.Kind, root)
				}
				if nil != err {
					return err
				}
			}
		}
	case reflect.Slice:
		if rv.IsNil() {
			return
		}
		fallthrough
	case reflect.Array:
		rt = rt.Elem()
		rk = rt.Kind()
		l := rv.Len()
		for i := 0; i < l; i++ {
			err = transValue(rv.Index(i), rt, rk, root)
			if nil != err {
				return
			}
		}
	case reflect.Map:
		if rv.IsNil() {
			return
		}
		rt = rt.Elem()
		rk = rt.Kind()
		for _, k := range rv.MapKeys() {
			err = transValue(rv.MapIndex(k), rt, rk, root)
			if nil != err {
				return
			}
		}
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
		rt = rv.Type()
		err = transValue(rv, rt, rt.Kind(), root)
		if nil != err {
			return
		}
	}
	return nil
}

func Trans(v interface{}) error {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr {
		return fmt.Errorf("not pointer")
	}
__loop:
	rv = rv.Elem()
	if rv.Kind() == reflect.Ptr {
		goto __loop
	}
	rt := rv.Type()
	rk := rt.Kind()
	return transValue(rv, rt, rk, unsafe.Pointer(rv.UnsafeAddr()))
}
