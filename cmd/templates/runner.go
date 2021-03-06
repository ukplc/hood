package main

import (
	"encoding/json"
	"github.com/eaigner/hood"
	"io/ioutil"
	"os"
	"path"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

import (
	"fmt"
)

type (
	M          struct{}
	Migrations struct {
		Id      hood.Id
		Current int
	}
	environments map[string]config
	config       map[string]string
)

func main() {
	// Determine direction
	up := true
	if len(os.Args) > 1 {
		if x := os.Args[1]; x == "down" {
			up = false
		}
	}
	if up {
		fmt.Println("applying migrations...")
	} else {
		fmt.Println("rolling back...")
	}
	// Get up/down migration methods
	v := reflect.ValueOf(&M{})
	numMethods := v.NumMethod()
	stamps := make([]int, 0, numMethods)
	ups := make(map[int]reflect.Method)
	downs := make(map[int]reflect.Method)
	for i := 0; i < numMethods; i++ {
		method := v.Type().Method(i)
		chunks := strings.Split(method.Name, "_")
		if l := len(chunks); l >= 3 {
			ts, _ := strconv.Atoi(chunks[l-2])
			direction := chunks[l-1]
			if strings.ToLower(direction) == "up" {
				ups[ts] = method
				stamps = append(stamps, ts)
			} else {
				downs[ts] = method
			}
		}
	}
	sort.Ints(stamps)
	// Get config for set environment
	env := "development"
	if x := os.Getenv("HOOD_ENV"); x != "" {
		env = x
	}
	cfg := readConfig()[env]
	// Open hood
	hd, err := hood.Open(cfg["driver"], cfg["source"])
	if err != nil {
		panic(err)
	}
	// Check migration table
	err = hd.CreateTableIfNotExists(&Migrations{})
	if err != nil {
		panic(err)
	}
	var rows []Migrations
	err = hd.Find(&rows)
	if err != nil {
		panic(err)
	}
	info := Migrations{}
	if len(rows) > 0 {
		info = rows[0]
	}
	runCount := 0
	for i, ts := range stamps {
		if up {
			if ts > info.Current {
				tx := hd.Begin()
				method := ups[ts]
				method.Func.Call([]reflect.Value{v, reflect.ValueOf(tx)})
				info.Current = ts
				tx.Save(&info)
				err = tx.Commit()
				if err != nil {
					panic(err)
				} else {
					runCount++
					fmt.Printf("applied %s\n", method.Name)
				}
			}
		} else {
			if info.Current == ts {
				tx := hd.Begin()
				method := downs[ts]
				method.Func.Call([]reflect.Value{v, reflect.ValueOf(tx)})
				if i > 0 {
					info.Current = stamps[i-1]
				} else {
					info.Current = 0
				}
				tx.Save(&info)
				err = tx.Commit()
				if err != nil {
					panic(err)
				} else {
					runCount++
					fmt.Printf("rolled back %s\n", method.Name)
					break
				}
			}
		}
	}
	if up {
		fmt.Printf("applied %d migrations\n", runCount)
	} else {
		fmt.Printf("rolled back %d migrations\n", runCount)
	}
	fmt.Println("generating new schema...")
	dry := hood.Dry()
	for _, ts := range stamps {
		if ts <= info.Current {
			method := ups[ts]
			method.Func.Call([]reflect.Value{v, reflect.ValueOf(dry)})
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	schema := fmt.Sprintf(
		"package db\n\nimport (\n\t\"github.com/eaigner/hood\"\n)\n\n%s",
		dry.SchemaDefinition(),
	)
	schemaPath := path.Join(wd, "db", "schema.go")
	err = ioutil.WriteFile(schemaPath, []byte(schema), 0666)
	if err != nil {
		panic(err)
	}
	fmt.Printf("wrote schema %s\n", schemaPath)
	fmt.Println("done.")
}

func readConfig() environments {
	wd, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	sf, err := os.Open(path.Join(wd, "db", "config.json"))
	if err != nil {
		panic(err)
	}
	defer sf.Close()
	dec := json.NewDecoder(sf)
	var env environments
	err = dec.Decode(&env)
	if err != nil {
		panic(err)
	}
	return env
}
