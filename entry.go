// Copyright 2015 Factom Foundation
// Use of this source code is governed by the MIT
// license that can be found in the LICENSE file.

package factom

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	ed "github.com/FactomProject/ed25519"
	"github.com/FactomProject/factomd/wsapi"
)

func NewEntry() *Entry {
	e := new(Entry)

	return e
}

// CommitEntry sends the signed Entry Hash and the Entry Credit public key to
// the factom network. Once the payment is verified and the network is commited
// to publishing the Entry it may be published with a call to RevealEntry.
func CommitEntry(e *Entry, name string) error {
	type walletcommit struct {
		Message string
	}

	buf := new(bytes.Buffer)

	// 1 byte version
	buf.Write([]byte{0})

	// 6 byte milliTimestamp (truncated unix time)
	buf.Write(milliTime())

	// 32 byte Entry Hash
	buf.Write(e.Hash())

	// 1 byte number of entry credits to pay
	if c, err := entryCost(e); err != nil {
		return err
	} else {
		buf.WriteByte(byte(c))
	}

	com := new(walletcommit)
	com.Message = hex.EncodeToString(buf.Bytes())
	j, err := json.Marshal(com)
	if err != nil {
		return err
	}
	resp, err := http.Post(
		fmt.Sprintf("http://%s/v1/commit-entry/%s", serverFct, name),
		"application/json",
		bytes.NewBuffer(j))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		p, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf(string(p))
	}

	return nil
}

func ComposeEntryCommit(pub *[32]byte, pri *[64]byte, e *Entry) ([]byte, error) {
	type commit struct {
		CommitEntryMsg string
	}

	buf := new(bytes.Buffer)

	// 1 byte version
	buf.Write([]byte{0})

	// 6 byte milliTimestamp (truncated unix time)
	buf.Write(milliTime())

	// 32 byte Entry Hash
	buf.Write(e.Hash())

	// 1 byte number of entry credits to pay
	if c, err := entryCost(e); err != nil {
		return nil, err
	} else {
		buf.WriteByte(byte(c))
	}

	// sign the commit
	sig := ed.Sign(pri, buf.Bytes())

	// 32 byte Entry Credit Public Key
	buf.Write(pub[:])

	// 64 byte Signature
	buf.Write(sig[:])

	com := new(commit)
	com.CommitEntryMsg = hex.EncodeToString(buf.Bytes())
	j, err := json.Marshal(com)
	if err != nil {
		return nil, err
	}

	return j, nil
}

func ComposeEntryReveal(e *Entry) ([]byte, error) {
	type reveal struct {
		Entry string
	}

	r := new(reveal)
	if p, err := e.MarshalBinary(); err != nil {
		return nil, err
	} else {
		r.Entry = hex.EncodeToString(p)
	}

	j, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}

	return j, nil
}

func RevealEntry(e *Entry) (*wsapi.RevealEntryResponse, error) {
	type reveal struct {
		Entry string
	}

	r := new(reveal)
	if p, err := e.MarshalBinary(); err != nil {
		return nil, err
	} else {
		r.Entry = hex.EncodeToString(p)
	}

	resp, err := CallV2("reveal-entry", false, r, new(wsapi.RevealEntryResponse))
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf(resp.Error.Message)
	}

	return resp.Result.(*wsapi.RevealEntryResponse), nil
}

func GetEntry(hash string) (*Entry, error) {
	resp, err := CallV2("entry-by-hash", false, hash, new(Entry))
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf(resp.Error.Message)
	}

	return resp.Result.(*Entry), nil
}

/**************************************************************************************/

type Entry wsapi.EntryResponse

/*
type Entry struct {
	ChainID string
	ExtIDs  [][]byte
	Content []byte
}*/

func (e *Entry) Hash() []byte {
	a, err := e.MarshalBinary()
	if err != nil {
		return make([]byte, 32)
	}
	return sha52(a)
}

func (e *Entry) MarshalBinary() ([]byte, error) {
	buf := new(bytes.Buffer)
	ids, err := e.MarshalExtIDsBinary()
	if err != nil {
		return buf.Bytes(), err
	}

	// Header

	// 1 byte Version
	buf.Write([]byte{0})

	// 32 byte chainid
	if p, err := hex.DecodeString(e.ChainID); err != nil {
		return buf.Bytes(), err
	} else {
		buf.Write(p)
	}

	// 2 byte size of extids
	if err := binary.Write(buf, binary.BigEndian, int16(len(ids))); err != nil {
		return buf.Bytes(), err
	}

	// Payload

	// ExtIDs
	buf.Write(ids)

	// Content
	buf.Write([]byte(e.Content))

	return buf.Bytes(), nil
}

func (e *Entry) MarshalExtIDsBinary() ([]byte, error) {
	buf := new(bytes.Buffer)

	for _, v := range e.ExtIDs {
		// 2 byte length of extid
		binary.Write(buf, binary.BigEndian, int16(len(v)))
		// extid
		buf.Write([]byte(v))
	}

	return buf.Bytes(), nil
}

func (e *Entry) MarshalJSON() ([]byte, error) {
	type js struct {
		ChainID string
		ExtIDs  []string
		Content string
	}

	j := new(js)

	j.ChainID = e.ChainID

	for _, id := range e.ExtIDs {
		j.ExtIDs = append(j.ExtIDs, id)
	}

	j.Content = e.Content

	return json.Marshal(j)
}

func (e *Entry) String() string {
	var s string
	s += fmt.Sprintln("ChainID:", e.ChainID)
	for _, id := range e.ExtIDs {
		s += fmt.Sprintln("ExtID:", id)
	}
	s += fmt.Sprintln("Content:")
	s += fmt.Sprintln(e.Content)
	return s
}

func (e *Entry) UnmarshalJSON(data []byte) error {
	type js struct {
		ChainID   string
		ChainName []string
		ExtIDs    []string
		Content   string
	}

	j := new(js)
	if err := json.Unmarshal(data, j); err != nil {
		return err
	}

	e.ChainID = j.ChainID

	if e.ChainID == "" {
		n := NewEntry()
		for _, v := range j.ChainName {
			n.ExtIDs = append(n.ExtIDs, v)
		}
		m := NewChain(n)
		e.ChainID = m.ChainID
	}

	for _, v := range j.ExtIDs {
		e.ExtIDs = append(e.ExtIDs, v)
	}

	e.Content = j.Content

	return nil
}

func entryCost(e *Entry) (int8, error) {
	p, err := e.MarshalBinary()
	if err != nil {
		return 0, err
	}

	// caulculaate the length exluding the header size 35 for Milestone 1
	l := len(p) - 35

	if l > 10240 {
		return 10, fmt.Errorf("Entry cannot be larger than 10KB")
	}

	// n is the capacity of the entry payment in KB
	r := l % 1024
	n := int8(l / 1024)

	if r > 0 {
		n += 1
	}

	if n < 1 {
		n = 1
	}
	return n, nil
}
