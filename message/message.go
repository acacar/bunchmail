/* Copyright (c) 2018 Aybar C. Acar <acacar@metu.edu.tr>
 *
 * Redistribution and use in source and binary forms, with or without
 * modification, are permitted provided that the following conditions are
 * met:
 *
 * 1. Redistributions of source code must retain the above copyright
 * notice, this list of conditions and the following disclaimer.
 *
 * 2. Redistributions in binary form must reproduce the above copyright
 * notice, this list of conditions and the following disclaimer in the
 * documentation and/or other materials provided with the distribution.
 *
 * 3. Neither the name of the copyright holder nor the names of its
 * contributors may be used to endorse or promote products derived from
 * this software without specific prior written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS
 * "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT
 * LIMITED TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR
 * A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT
 * HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT
 * LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE,
 * DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON ANY
 * THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
 * (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 * OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package message

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	"net/mail"
	"os"
	"regexp"
	"strings"
	"time"
)

type Message struct {
	message     *mail.Message
	Subject     string
	Filename    string
	ID          string
	Timestamp   time.Time
	IsDupe      bool
	NoID        bool
	NoTimestamp bool
	Flags       string
	From        string
}

func (msg *Message) SaveTo(dirPath string, domain string, removeFlags string, cntChan chan int) error {
	removeFlags = strings.ToUpper(removeFlags)
	cnt := <-cntChan

	var newbase string = fmt.Sprintf("%d.%09d.%s", msg.Timestamp.Unix(), cnt, domain)

	keptFlags := msg.Flags
	for _, l := range removeFlags {
		keptFlags = strings.Replace(msg.Flags, string(l), "", -1)
	}

	savepath := fmt.Sprintf("%s:2,%s", newbase, keptFlags)
	if keptFlags == "" {
		savepath = dirPath + "/new/" + savepath
	} else {
		savepath = dirPath + "/cur/" + savepath
	}

	err := copyFileContents(msg.Filename, savepath)
	if err != nil {
		return err
	}

	// Make the mtime fit the actual time of message
	err = os.Chtimes(savepath, msg.Timestamp, msg.Timestamp)
	if err != nil {
		return err
	}
	return nil
}

func (msg *Message) sender() (string, error) {
	m := msg.message

	senderaddr, err := mail.ParseAddress(m.Header.Get("From"))

	if err != nil {
		// There are usually problems reading weird charsets like iso-8859-9 and koi8, just ignore them
		//log.Printf("Can't parse From header from %s => %s", msg.Filename, err.Error())
		return "", nil
	}

	return senderaddr.Address, nil
}

func (msg *Message) messageID() string {
	m := msg.message
	rawmid := m.Header.Get("Message-ID")
	if len(rawmid) == 0 {
		log.Println("No message-id for " + msg.Filename)
	}
	return strings.Trim(rawmid, " <>\t\n")
}

func (msg *Message) subject() string {
	m := msg.message
	rawsub := m.Header.Get("Subject")
	//	if len(rawsub) == 0 {
	//		log.Println("No subject for " + msg.Filename)
	//	}
	return rawsub
}

func (msg *Message) actualTime() (time.Time, error) {
	m := msg.message
	recheaders := m.Header["Received"]
	bestguesstime := time.Unix(0, 0).UTC()
	if len(recheaders) < 1 {
		dateheadertime, err := timeFromDateHeaderOf(m)
		if err == nil {
			bestguesstime = dateheadertime
			return dateheadertime, nil
		}
		log.Println("bunchmail: Having trouble finding the date of", msg.Filename)
		return bestguesstime, nil
	}

	latest, err := timeFromReceivedHeaderString(recheaders[0])
	if err != nil {
		log.Println(" Having trouble parsing the first received date of " + msg.Filename + ", falling back to the 'Date:' header")
		return bestguesstime, nil
	}

	for i := 1; i < len(recheaders); i++ {
		t, err := timeFromReceivedHeaderString(recheaders[i])
		if err != nil {

			//log.Println("Having trouble parsing the remaining received dates of " + msg.Filename)
			continue
		}
		if t.After(latest) {
			latest = t
		}
	}

	return latest, nil
}

func (msg *Message) loadfromfile(filename string) error {
	msg.Filename = filename
	f, err := os.Open(msg.Filename)
	if err != nil {
		return err
	}
	defer f.Close()

	msg.Flags = strings.Split(filename, ":2,")[1]

	mio := bufio.NewReader(f)
	m, err := mail.ReadMessage(mio)
	if err != nil {
		return err
	}
	msg.message = m

	actualtime, err := msg.actualTime()
	if err != nil {
		return err
	}
	msg.Timestamp = actualtime

	msgid := msg.messageID()
	msg.ID = msgid

	msg.Subject = msg.subject()

	msg.From, err = msg.sender()
	if err != nil {
		return err
	}

	return nil
}

func New(filename string, domain string) (Message, error) {

	msg := Message{}
	err := msg.loadfromfile(filename)

	if err != nil {
		return msg, err
	}

	if msg.ID == "" {
		newid, err := createNewMessageID(filename, domain)
		if err != nil {
			log.Fatal("Can't continue b/c of: " + err.Error())
		}
		log.Printf("%v has no Message-ID, creating a new one: %v\n", filename, newid)
		msg.ID = newid
		msg.NoID = true
	}

	if msg.Timestamp == time.Unix(0, 0).UTC() {
		log.Printf("%v has no timestamp information, using Unix epoch\n", filename)
		msg.NoTimestamp = true
	}

	msg.message = nil
	return msg, nil

}

// Utility functions

func timeFromReceivedHeaderString(received string) (time.Time, error) {
	parts := strings.Split(received, ";")
	rfctimestr := strings.Trim(parts[len(parts)-1], " \t")
	re := regexp.MustCompile("\\s\\(.+\\)")
	rfctimestr = re.ReplaceAllString(rfctimestr, "")
	msgtime, err := mail.ParseDate(rfctimestr)
	if err == nil {
		return msgtime, nil
	}

	return msgtime, err
}

func timeFromDateHeaderOf(m *mail.Message) (time.Time, error) {
	dateheader := m.Header.Get("Date")
	return mail.ParseDate(dateheader)
}

func createNewMessageID(filename string, domain string) (string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		log.Fatal(err)
	}
	return fmt.Sprintf("%x@%v", h.Sum(nil), domain), nil
}

func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return
	}
	err = out.Sync()
	return
}
