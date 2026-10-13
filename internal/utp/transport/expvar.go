package transport

// GEXTTO: our own uTP (BEP 29) code; see doc.go. expvar counters for the uTP stack, exported on the
// process's /debug/vars page for diagnostics.

import "expvar"

var (
	// ackSkippedResends counts selective-ACK fast resends.
	ackSkippedResends = expvar.NewInt("utpAckSkippedResends")
	// deliveriesProcessed counts inbound packets handled by a Conn.
	deliveriesProcessed = expvar.NewInt("utpDeliveriesProcessed")
	// sentStatePackets counts state (ACK) packets sent.
	sentStatePackets = expvar.NewInt("utpSentStatePackets")
	// acksReceivedAheadOfSyn counts ACKs for packets we had not sent yet.
	acksReceivedAheadOfSyn = expvar.NewInt("utpAcksReceivedAheadOfSyn")
	// unexpectedPacketsRead counts packets with no attached connection.
	unexpectedPacketsRead = expvar.NewInt("utpUnexpectedPacketsRead")
	// unsentStatePackets counts state packets we avoided sending.
	unsentStatePackets = expvar.NewInt("utpUnsentStatePackets")
	// unusedReads counts reads diverted to no connection.
	unusedReads = expvar.NewInt("utpUnusedReads")
	// unusedReadsDropped counts reads dropped for lack of a reader.
	unusedReadsDropped = expvar.NewInt("utpUnusedReadsDropped")

	// largestReceivedUTPPacket is the largest packet seen, exported below.
	largestReceivedUTPPacket       int
	largestReceivedUTPPacketExpvar = expvar.NewInt("utpLargestReceivedPacket")
)
