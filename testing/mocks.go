// Copyright 2022 Google LLC
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

package testing

import (
	"encoding/hex"
	"fmt"

	"github.com/google/go-sev-guest/abi"
	labi "github.com/google/go-sev-guest/client/linuxabi"
	"github.com/pkg/errors"
)

// GetReportResponse represents a mocked response to a command request.
type GetReportResponse struct {
	Resp     labi.SnpReportRespABI
	EsResult labi.EsResult
	FwErr    abi.SevFirmwareStatus
}

// Device represents a sev-guest driver implementation with pre-programmed responses to commands.
type Device struct {
	isOpen      bool
	UserDataRsp map[string]interface{}
	Certs       []byte
	Signer      *AmdSigner
}

// Open changes the mock device's state to open.
func (d *Device) Open(path string) error {
	if d.isOpen {
		return errors.New("device already open")
	}
	d.isOpen = true
	return nil
}

// Close changes the mock device's state to closed.
func (d *Device) Close() error {
	if !d.isOpen {
		return errors.New("device already closed")
	}
	d.isOpen = false
	return nil
}

func (d *Device) getReport(req *labi.SnpReportReqABI, rsp *labi.SnpReportRespABI, fwErr *uint64) (uintptr, error) {
	mockRspI, ok := d.UserDataRsp[hex.EncodeToString(req.UserData[:])]
	if !ok {
		return 0, fmt.Errorf("test error: no response for %v", req.UserData)
	}
	mockRsp, ok := mockRspI.(*GetReportResponse)
	if !ok {
		return 0, fmt.Errorf("test error: incorrect response type %v", mockRspI)
	}
	esResult := uintptr(mockRsp.EsResult)
	if mockRsp.FwErr != 0 {
		*fwErr = uint64(mockRsp.FwErr)
		return esResult, nil
	}
	report := mockRsp.Resp.Data[:abi.ReportSize]
	r, s, err := d.Signer.Sign(abi.SignedComponent(report))
	if err != nil {
		return 0, fmt.Errorf("test error: could not sign report: %v", err)
	}
	if err := abi.SetSignature(r, s, report); err != nil {
		return 0, fmt.Errorf("test error: could not set signature: %v", err)
	}
	copy(rsp.Data[:], report)
	return esResult, nil
}

func (d *Device) getExtReport(req *labi.SnpExtendedReportReq, rsp *labi.SnpReportRespABI, fwErr *uint64) (uintptr, error) {
	if req.CertsLength == 0 {
		*fwErr = uint64(abi.GuestRequestInvalidLength)
		req.CertsLength = uint32(len(d.Certs))
		return 0, nil
	}
	ret, err := d.getReport(&req.Data, rsp, fwErr)
	if err != nil {
		return ret, err
	}
	if req.CertsLength < uint32(len(d.Certs)) {
		return 0, fmt.Errorf("test failure: cert buffer too small: %d < %d", req.CertsLength, len(d.Certs))
	}
	copy(req.Certs, d.Certs)
	return ret, nil
}

// Ioctl mocks commands with pre-specified responses for a finite number of requests.
func (d *Device) Ioctl(command uintptr, req interface{}) (uintptr, error) {
	switch sreq := req.(type) {
	case *labi.SnpUserGuestRequest:
		switch command {
		case labi.IocSnpGetReport:
			return d.getReport(sreq.ReqData.(*labi.SnpReportReqABI), sreq.RespData.(*labi.SnpReportRespABI), &sreq.FwErr)
		case labi.IocSnpGetExtendedReport:
			return d.getExtReport(sreq.ReqData.(*labi.SnpExtendedReportReq), sreq.RespData.(*labi.SnpReportRespABI), &sreq.FwErr)
		default:
			return 0, fmt.Errorf("invalid command 0x%x", command)
		}
	}
	return 0, fmt.Errorf("unexpected request: %v", req)
}

// Getter represents a static server for request/respond url -> body contents.
type Getter struct {
	Responses map[string][]byte
}

// Get returns a registered response for a given URL.
func (g *Getter) Get(url string) ([]byte, error) {
	v, ok := g.Responses[url]
	if !ok {
		return nil, fmt.Errorf("404: %s", url)
	}
	return v, nil
}
