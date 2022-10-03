package coredns_filter

import (
	"testing"

	"golang.org/x/net/context"

	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"

	"github.com/miekg/dns"
)

// testHandler implements HandlerWithCallbacks to mock handler
type testHandler struct {
	rcode   int
	called  int
	answers []dns.RR
}

// newTestHandler sets up handler (forward plugin) mock. It returns rcode defined in parameter.
func newTestHandler(rcode int, answers []dns.RR) *testHandler {
	return &testHandler{rcode: rcode, answers: answers}
}

func (h *testHandler) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	h.called++
	ret := new(dns.Msg)
	ret.SetReply(r)
	ret.Answer = h.answers
	ret.Rcode = h.rcode
	w.WriteMsg(ret)
	return 0, nil
}

func (h *testHandler) Name() string      { return "testHandler" }
func (h *testHandler) OnStartup() error  { return nil }
func (h *testHandler) OnShutdown() error { return nil }

// makeTestCall makes test call to handler
func makeTestCall(handler *Filter) (*dnstest.Recorder, int, error) {
	// Prepare query and make a call
	ctx := context.TODO()
	req := &dns.Msg{
		Question: []dns.Question{{
			Name:   "example.org.",
			Qclass: dns.ClassINET,
			Qtype:  dns.TypeA,
		}, {
			Name:   "example.org.",
			Qclass: dns.ClassINET,
			Qtype:  dns.TypeAAAA,
		}},
	}

	rec := dnstest.NewRecorder(&test.ResponseWriter{})
	rcode, err := handler.ServeDNS(ctx, rec, req)
	return rec, rcode, err
}

// Test case for filter
type filterTestCase struct {
	configure       [][]string
	nextRcode       int // rcode to be returned by the stub Handler
	expectedRcode   int // this is expected rcode by test handler (forward plugin)
	called          int // this is expected number of calls reached test filter server
	nextAnswers     []dns.RR
	expectedAnswers []dns.RR
}

func TestFilter(t *testing.T) {
	testCases := []filterTestCase{
		{
			configure: [][]string{
				{"bogus-nxdomain", "172.16.1.0/24", "fe80::2896:acff:fe37:1912"},
			},
			nextRcode:     dns.RcodeSuccess,
			expectedRcode: dns.RcodeSuccess,
			called:        1,
			nextAnswers: []dns.RR{
				test.A("example.org. IN A 127.0.0.1"),
				test.A("example.org. IN A 172.16.1.1"),
				test.AAAA("example.org. IN AAAA fe80::2896:acff:fe37:1912"),
				test.AAAA("example.org. IN AAAA 2606:4700:3034::ac43:dece"),
			},
			expectedAnswers: []dns.RR{
				test.A("example.org. IN A 127.0.0.1"),
				test.AAAA("example.org. IN AAAA 2606:4700:3034::ac43:dece"),
			},
		},
		{
			configure: [][]string{
				{"bogus-nxdomain", "172.16.1.1", "fe80::/8"},
			},
			nextRcode:     dns.RcodeSuccess,
			expectedRcode: dns.RcodeNameError,
			called:        1,
			nextAnswers: []dns.RR{
				test.A("example.org. IN A 172.16.1.1"),
				test.AAAA("example.org. IN AAAA fe80::2896:acff:fe37:1912"),
			},
			expectedAnswers: []dns.RR{},
		},
		{
			configure: [][]string{
				{"prefer", "ipv4"},
			},
			nextRcode:     dns.RcodeSuccess,
			expectedRcode: dns.RcodeSuccess,
			called:        1,
			nextAnswers: []dns.RR{
				test.A("example.org. IN A 127.0.0.1"),
				test.A("example.org. IN A 172.16.10.1"),
				test.AAAA("example.org. IN AAAA fe80::2896:acff:fe37:1912"),
				test.AAAA("example.org. IN AAAA fe80::a236:9f96:507:3f9b"),
			},
			expectedAnswers: []dns.RR{
				test.A("example.org. IN A 127.0.0.1"),
				test.A("example.org. IN A 172.16.10.1"),
			},
		},
		{
			configure: [][]string{
				{"prefer", "ipv6"},
			},
			nextRcode:     dns.RcodeSuccess,
			expectedRcode: dns.RcodeSuccess,
			called:        1,
			nextAnswers: []dns.RR{
				test.A("example.org. IN A 127.0.0.1"),
				test.A("example.org. IN A 172.16.10.1"),
				test.AAAA("example.org. IN AAAA fe80::2896:acff:fe37:1912"),
				test.AAAA("example.org. IN AAAA fe80::a236:9f96:507:3f9b"),
			},
			expectedAnswers: []dns.RR{
				test.AAAA("example.org. IN AAAA fe80::2896:acff:fe37:1912"),
				test.AAAA("example.org. IN AAAA fe80::a236:9f96:507:3f9b"),
			},
		},
	}

	for testNum, tc := range testCases {
		// mocked Forward for servicing a specific rcode
		h := newTestHandler(tc.nextRcode, tc.nextAnswers)

		handler := NewFilterHandler()
		// create stub handler to return the test rcode
		handler.Next = h

		// add rules
		for _, opt := range tc.configure {
			err := handler.AddCommand(opt[0], opt[1:])
			if err != nil {
				t.Errorf("Test '%d': Filter AddCommand '%v' error '%v'.", testNum, opt, err)
			}
		}

		// Prepare query and make a call
		records, rcode, err := makeTestCall(&handler)

		// Ensure that no errors returned
		if rcode != tc.expectedRcode || records.Rcode != tc.expectedRcode || err != nil {
			t.Errorf("Test '%d': Filter returned code '%d' error '%v'. Expected '%v' and no error",
				testNum, rcode, err, tc.expectedRcode)
		}

		// Ensure that all answers is correct
		if records.Msg != nil {
			if len(records.Msg.Answer) != len(tc.expectedAnswers) {
				t.Errorf("Test '%d': Filter returned %d answers. Expected %d answers",
					testNum, len(records.Msg.Answer), len(tc.expectedAnswers))
			}

			for i, answer := range records.Msg.Answer {
				if i >= len(tc.expectedAnswers) {
					t.Errorf("Test '%d': Filter got NO.%d answer '%v'. Expected nothing",
						testNum, i, answer)
				} else if answer.String() != tc.expectedAnswers[i].String() {
					t.Errorf("Test '%d': Filter got NO.%d answer '%v'. Expected '%v'",
						testNum, i, answer, tc.expectedAnswers[i])
				}
			}
		}

		// Ensure that server was called required number of times
		if h.called != tc.called {
			t.Errorf("Test '%d': Server expected to be called %d time(s) but called %d times(s)",
				testNum, tc.called, h.called)
		}
	}
}
