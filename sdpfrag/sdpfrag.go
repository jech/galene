// Package sdpfrag is an incomplete implementation of RFC 8840 Section 9
package sdpfrag

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"

	"github.com/pion/sdp/v3"
	"github.com/pion/webrtc/v4"
)

// An SDPFrag contains the ICE-related subset of a session description.
type SDPFrag struct {
	UsernameFragment, Password string
	Candidates                 []webrtc.ICECandidateInit
	MediaDescriptions          []MediaDescription
}

type MediaDescription struct {
	MLine                      string
	Mid                        string
	UsernameFragment, Password string
	Candidates                 []webrtc.ICECandidateInit
}

func (s *SDPFrag) Unmarshal(value []byte) error {
	scanner := bufio.NewScanner(bytes.NewReader(value))

	var mediaDescription *MediaDescription

	for scanner.Scan() {
		l := scanner.Bytes()
		if bytes.HasPrefix(l, []byte("a=ice-ufrag:")) {
			ufrag := string(l[len("a=ice-ufrag:"):])
			if mediaDescription == nil {
				s.UsernameFragment = ufrag
			} else {
				mediaDescription.UsernameFragment = ufrag
			}
		} else if bytes.HasPrefix(l, []byte("a=ice-pwd:")) {
			pwd := string(l[len("a=ice-pwd:"):])
			if mediaDescription == nil {
				s.Password = pwd
			} else {
				mediaDescription.Password = pwd
			}
		} else if bytes.HasPrefix(l, []byte("m=")) {
			if mediaDescription != nil {
				s.MediaDescriptions = append(
					s.MediaDescriptions,
					*mediaDescription,
				)
			}
			mediaDescription = &MediaDescription{
				MLine: string(l[len("m="):]),
			}
		} else if bytes.HasPrefix(l, []byte("a=mid:")) {
			if mediaDescription == nil {
				return errors.New("unexpected mid")
			}
			mediaDescription.Mid = string(l[len("a=mid:"):])
		} else if bytes.HasPrefix(l, []byte("a=candidate:")) {
			init := webrtc.ICECandidateInit{
				Candidate: string(l[len("a=candidate:"):]),
			}
			if len(s.UsernameFragment) > 0 {
				ufrag := s.UsernameFragment
				init.UsernameFragment = &ufrag
			}
			if mediaDescription != nil {
				i := uint16(len(s.MediaDescriptions))
				init.SDPMLineIndex = &i
				m := mediaDescription.Mid
				init.SDPMid = &m
				mediaDescription.Candidates = append(
					mediaDescription.Candidates,
					init,
				)
			} else {
				s.Candidates = append(s.Candidates, init)
			}
		}
	}
	if mediaDescription != nil {
		s.MediaDescriptions = append(
			s.MediaDescriptions,
			*mediaDescription,
		)
	}
	return nil
}

func (s *SDPFrag) Marshal() ([]byte, error) {
	w := new(bytes.Buffer)

	if s.UsernameFragment != "" {
		fmt.Fprintf(w, "a=ice-ufrag:%v\r\n", s.UsernameFragment)
	}
	if s.Password != "" {
		fmt.Fprintf(w, "a=ice-pwd:%v\r\n", s.Password)
	}
	for _, c := range s.Candidates {
		fmt.Fprintf(w, "a=%v\r\n", c.Candidate)
	}
	for _, m := range s.MediaDescriptions {
		fmt.Fprintf(w, "m=%v\r\n", m.MLine)
		fmt.Fprintf(w, "a=mid:%v\r\n", m.Mid)
		if m.UsernameFragment != "" {
			fmt.Fprintf(w, "a=ice-ufrag:%v\r\n", m.UsernameFragment)
		}
		if m.Password != "" {
			fmt.Fprintf(w, "a=ice-pwd:%v\r\n", m.Password)
		}
		for _, c := range m.Candidates {
			fmt.Fprintf(w, "a=candidate:%v\r\n", c.Candidate)
		}
	}

	return w.Bytes(), nil
}

func patch(ufrag, pwd string, old []sdp.Attribute, cs []webrtc.ICECandidateInit, override bool) []sdp.Attribute {
	var as []sdp.Attribute
	for _, a := range old {
		if a.Key == "ice-ufrag" {
			if ufrag == "" {
				continue
			}
			as = append(as, sdp.Attribute{
				Key:   "ice-ufrag",
				Value: ufrag,
			})
		} else if a.Key == "ice-pwd" {
			if pwd == "" {
				continue
			}
			as = append(as, sdp.Attribute{
				Key:   "ice-pwd",
				Value: pwd,
			})
		} else if !override || a.Key != "candidate" {
			as = append(as, a)
		}
	}
	for _, c := range cs {
		as = append(as, sdp.Attribute{
			Key:   "candidate",
			Value: c.Candidate,
		})
	}
	return as
}

// SDPUFragPwd returns the ICE ufrag and password of an SDP.
// In principle, the ufrag/pwd should be at the same place in the
// frag as in the original SDP, RFC 8840 Section 9.  However, we
// only do bundle, and some implementations put the data at random
// places, so we walk the whole structure to find the ufrag/pwd pair.
func SDPUFragPwd(s sdp.SessionDescription) (string, string) {
	ufrag, _ := s.Attribute("ice-ufrag")
	pwd, _ := s.Attribute("ice-pwd")
	if ufrag != "" {
		return ufrag, pwd
	}
	for _, m := range s.MediaDescriptions {
		ufrag, _ := m.Attribute("ice-ufrag")
		pwd, _ := m.Attribute("ice-pwd")
		if ufrag != "" {
			return ufrag, pwd
		}
	}
	return "", ""
}

// UFragPwd returns the ICE ufrag and password of a fragment.
// See the description of [SDPUFragPwd] for details.
func (f *SDPFrag) UFragPwd() (string, string) {
	ufrag := f.UsernameFragment
	pwd := f.Password
	if ufrag != "" {
		return ufrag, pwd
	}
	for _, m := range f.MediaDescriptions {
		ufrag := m.UsernameFragment
		pwd := m.Password
		if ufrag != "" {
			return ufrag, pwd
		}
	}
	return "", ""
}

func (f *SDPFrag) AllCandidates() []webrtc.ICECandidateInit {
	var cs []webrtc.ICECandidateInit
	cs = append(cs, f.Candidates...)
	for _, m := range f.MediaDescriptions {
		cs = append(cs, m.Candidates...)
	}
	return cs
}

// PatchSDP applies an [SDPFrag] to an [sdp.SessionDescription],
// either adding or replacing candidates depending on the ufrag/password
// values.
func PatchSDP(s sdp.SessionDescription, f SDPFrag) (sdp.SessionDescription, bool) {
	ufrag, pwd := SDPUFragPwd(s)
	ufrag1, pwd1 := f.UFragPwd()

	override := (ufrag != ufrag1) || (pwd != pwd1)

	s.Attributes = patch(ufrag1, pwd1, s.Attributes, f.Candidates, override)

	old := s.MediaDescriptions
	s.MediaDescriptions = nil
	for i, m := range old {
		var mm MediaDescription
		if i < len(f.MediaDescriptions) {
			mm = f.MediaDescriptions[i]
		}
		m.Attributes = patch(
			ufrag1, pwd1, m.Attributes, mm.Candidates, override,
		)
		s.MediaDescriptions = append(s.MediaDescriptions, m)
	}
	return s, override
}
