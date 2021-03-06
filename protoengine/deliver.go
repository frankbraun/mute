// Copyright (c) 2015 Mute Communications Ltd.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package protoengine

import (
	"fmt"
	"io"
	"io/ioutil"

	"github.com/mutecomm/mute/encode/base64"
	"github.com/mutecomm/mute/log"
	"github.com/mutecomm/mute/mix/client"
)

func (pe *ProtoEngine) deliver(statusfp io.Writer, r io.Reader) error {
	enc, err := ioutil.ReadAll(r)
	if err != nil {
		return log.Error(err)
	}
	var mm client.MessageMarshalled
	mm, err = base64.Decode(string(enc))
	if err != nil {
		return log.Error(err)
	}
	messageOut, err := mm.Unmarshal().Deliver()
	if err != nil {
		if messageOut.Resend {
			log.Info("write: RESEND:\t%s", err.Error())
			fmt.Fprintf(statusfp, "RESEND:\t%s\n", err.Error())
			return nil
		}
		return log.Error(err)
	}
	return nil
}
