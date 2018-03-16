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

package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"github.com/acacar/bunchmail/message"
	"io/ioutil"
	"log"
	"os"
	"strings"
)

func check(e error) {

	if e != nil {
		panic(e)
	}

}

type multiarg []string

var dupmap map[string]bool = make(map[string]bool)

var bunchDirPath string
var noDupes bool
var sendIdents multiarg
var inboxPaths multiarg
var archivePaths multiarg
var remFlags string
var domain string
var dupcnt int
var nodatecnt int
var noidcnt int
var count int
var c chan int = make(chan int)

var inbox []*message.Message = make([]*message.Message, 0, 50000)
var archive []*message.Message = make([]*message.Message, 0, 50000)
var sent []*message.Message = make([]*message.Message, 0, 50000)

func (s *multiarg) String() string {
	return fmt.Sprint(*s)
}

func (s *multiarg) Set(value string) error {
	if len(*s) > 0 {
		return errors.New("argument already set")
	}
	for _, part := range strings.Split(value, ",") {
		*s = append(*s, part)
	}
	return nil
}

func flagsInit() {

	flag.StringVar(&domain, "domain", "bunchmail.local", "the domain to use for message files")
	flag.StringVar(&bunchDirPath, "bunchpath", "", "The directory path to the bunch maildir")
	flag.BoolVar(&noDupes, "nodupes", false, "If true, duplicate messages are discarded")
	flag.Var(&sendIdents, "identities", "Comma separated list of all mail addresses you use(d) to send mail (for sent mail collation)")
	flag.Var(&inboxPaths, "inboxes", "Comma delimited list of Inbox paths")
	flag.Var(&archivePaths, "archives", "Comma separated list of Archive maildir paths")

	flag.StringVar(&remFlags, "removeflags", "", "Flags to remove from mail (e.g. \"FRT\") ")
}

func makeBunchdir(dirpath string) {
	err := os.RemoveAll(dirpath)
	if err != nil {
		log.Fatalln("Cannot remove old bunch directory: " + err.Error())
	}

	for _, box := range [3]string{"Inbox", "Sent", "Archive"} {
		for _, subdir := range [3]string{"cur", "new", "tmp"} {
			err = os.MkdirAll(fmt.Sprintf("%s/%s/%s", dirpath, box, subdir), 0700)
			if err != nil {
				log.Fatalln(err.Error())
			}
		}
	}

}

func checkRequiredFlags() {
	if !flag.Parsed() {
		fmt.Println("Cannot parse commmand line options")
		flag.Usage()
		log.Fatalln("Cannot continue")
	}

	if bunchDirPath == "" {
		fmt.Println("You must give a bunch (output) directory")
		flag.Usage()
		log.Fatalln("Cannot continue")
	}

	if len(inboxPaths) == 0 {
		fmt.Println("You must define the (input) Inbox directories")
		flag.Usage()
		log.Fatalln("Cannot continue")
	}

	if len(archivePaths) == 0 {
		fmt.Println("You must define the (input) Archive directories")
		flag.Usage()
		log.Fatalln("Cannot continue")
	}

}

var dupewriter *bufio.Writer

func main() {

	flagsInit()

	flag.Parse()

	checkRequiredFlags()
	//fmt.Println(inboxPaths)

	//return

	confirmation_template := `
Using the following settings:

------
Output Maildir: %s
(The output Maildir will be cleared!!!)

Inboxes to bunch: %v

Archive boxes to bunch: %v

Your identities: %v

Flags to remove: %s

Eliminate duplicates: %v
-----

Is this OK? (y/n): 
`

	fmt.Printf(confirmation_template, bunchDirPath, inboxPaths, archivePaths, sendIdents, remFlags, noDupes)
	answer := "N"
	fmt.Scanln(&answer)
	answer = strings.Trim(strings.ToUpper(answer), " ")
	if answer != "Y" {
		os.Exit(2)
	}

	makeBunchdir(bunchDirPath)

	//Provide a counter for message SaveTo (for uniqueness)
	go func() {
		for i := 0; ; i++ {
			c <- i
		}
	}()

	dupefile, err := os.Create("dupes.log")
	check(err)
	defer dupefile.Close()
	dupewriter = bufio.NewWriter(dupefile)
	fmt.Fprintln(dupewriter, "Message-ID\tFrom\tSubject\tTime\tFilename\n")

	for _, d := range inboxPaths {

		processMail(d+"/new", false)
		processMail(d+"/cur", false)
		processMail(d+"/tmp", false)
	}

	for _, d := range archivePaths {

		processMail(d+"/new", true)
		processMail(d+"/cur", true)
		processMail(d+"/tmp", true)
	}

	boxes := map[string][]*message.Message{"Inbox": inbox, "Sent": sent, "Archive": archive}

	for name, box := range boxes {
		boxcnt := 0
		for _, m := range box {
			if boxcnt > 1 && boxcnt%1000 == 0 {
				log.Printf("%s: Wrote %d messages\n", name, boxcnt)
			}
			if !noDupes || !m.IsDupe {
				err := m.SaveTo(fmt.Sprintf("%s/%s", bunchDirPath, name), domain, remFlags, c)
				if err != nil {
					log.Fatal(err)
				}
				boxcnt++
			}
		}
		log.Printf("%s: Wrote %d of %d messages (rest were dupes)\n", name, boxcnt, len(box))
	}

	fmt.Printf("Total Messages: %d Duplicates: %d No Date: %d No Message-ID: %d\n", count, dupcnt, nodatecnt, noidcnt)
	fmt.Printf("Size of inbox: %d, Size of archives: %d, Size of sent: %d", len(inbox), len(archive), len(sent))
}

func processMail(path string, isArchive bool) {

	files, err := ioutil.ReadDir(path)
	check(err)

	for _, finfo := range files {

		if count > 1 && count%1000 == 0 {
			log.Printf("Read %d messages\n", count)
		}

		if finfo.IsDir() {
			log.Fatalf("There shouldn't be a directory here! (%v)", finfo.Name())
		}
		count++
		filepath := path + "/" + finfo.Name()
		msg, err := message.New(filepath, domain)
		if err != nil {
			log.Fatalf("Problem with message %s:\n\t%v", filepath, err)
		}

		if dupmap[msg.ID] {
			msg.IsDupe = true
			fmt.Fprintf(dupewriter, "%s\t%s\t%s\t%s\t%s\n ", msg.ID, msg.From, msg.Subject, msg.Timestamp, msg.Filename)
		} else {
			dupmap[msg.ID] = true
		}

		if msg.IsDupe {
			dupcnt++
		}

		if msg.NoTimestamp {
			nodatecnt++
		}

		if msg.NoID {
			noidcnt++
		}

		if slicecontains(sendIdents, msg.From) {
			sent = append(sent, &msg)
		} else if isArchive {
			archive = append(archive, &msg)
		} else {
			inbox = append(inbox, &msg)
		}

		//err = msg.SaveTo("/tmp/hede", domain, "S", &c)
		//if err != nil {
		//	log.Fatal(err)
		//}
	}
}

func slicecontains(slice []string, s string) bool {
	for _, ss := range slice {
		if strings.ToUpper(ss) == strings.ToUpper(s) {
			return true
		}
	}
	return false
}
