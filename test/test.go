// Copyright 2023 The Serverless Workflow Specification Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package test

type TestBAlias = []*TestB

type Test struct {
	Key              string
	Tas              int
	TestA            TestA
	TestB            *TestB
	TestBList        []TestB
	TestBListPointer []*TestB
	TestBAlias       TestBAlias
}

// +builder-gen:new-call=Test1Tag,Test2Tag
type TestA struct {
	TestB TestB
}

func (t *TestA) Test1Tag() {

}

func (t *TestA) Test2Tag() {

}

// +builder-gen:new-call=TestTag
type TestB struct {
	TestBKey string
}

func (t *TestB) TestTag() {

}

// +builder-gen:ignore=true
type TestC struct {
	Key int
}
