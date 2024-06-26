// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/armadaproject/armada/internal/scheduler (interfaces: SubmitScheduleChecker)

// Package mocks is a generated GoMock package.
package mocks

import (
	reflect "reflect"

	jobdb "github.com/armadaproject/armada/internal/scheduler/jobdb"
	armadaevents "github.com/armadaproject/armada/pkg/armadaevents"
	gomock "github.com/golang/mock/gomock"
)

// MockSubmitScheduleChecker is a mock of SubmitScheduleChecker interface.
type MockSubmitScheduleChecker struct {
	ctrl     *gomock.Controller
	recorder *MockSubmitScheduleCheckerMockRecorder
}

// MockSubmitScheduleCheckerMockRecorder is the mock recorder for MockSubmitScheduleChecker.
type MockSubmitScheduleCheckerMockRecorder struct {
	mock *MockSubmitScheduleChecker
}

// NewMockSubmitScheduleChecker creates a new mock instance.
func NewMockSubmitScheduleChecker(ctrl *gomock.Controller) *MockSubmitScheduleChecker {
	mock := &MockSubmitScheduleChecker{ctrl: ctrl}
	mock.recorder = &MockSubmitScheduleCheckerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockSubmitScheduleChecker) EXPECT() *MockSubmitScheduleCheckerMockRecorder {
	return m.recorder
}

// CheckApiJobs mocks base method.
func (m *MockSubmitScheduleChecker) CheckApiJobs(arg0 *armadaevents.EventSequence, arg1 string) (bool, string) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CheckApiJobs", arg0, arg1)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(string)
	return ret0, ret1
}

// CheckApiJobs indicates an expected call of CheckApiJobs.
func (mr *MockSubmitScheduleCheckerMockRecorder) CheckApiJobs(arg0, arg1 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CheckApiJobs", reflect.TypeOf((*MockSubmitScheduleChecker)(nil).CheckApiJobs), arg0, arg1)
}

// CheckJobDbJobs mocks base method.
func (m *MockSubmitScheduleChecker) CheckJobDbJobs(arg0 []*jobdb.Job) (bool, string) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CheckJobDbJobs", arg0)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(string)
	return ret0, ret1
}

// CheckJobDbJobs indicates an expected call of CheckJobDbJobs.
func (mr *MockSubmitScheduleCheckerMockRecorder) CheckJobDbJobs(arg0 interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CheckJobDbJobs", reflect.TypeOf((*MockSubmitScheduleChecker)(nil).CheckJobDbJobs), arg0)
}
