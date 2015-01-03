/*
Copyright (c) 2014 Ashley Jeffs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
THE SOFTWARE.
*/

package leaplib

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sync"
	"testing"
	"time"
)

func TestNewBinder(t *testing.T) {
	errChan := make(chan BinderError)
	doc := CreateNewDocument("test", "test1", "hello world")
	binder, err := BindNew(doc, &MemoryStore{documents: map[string]*Document{}}, DefaultBinderConfig(), errChan)

	if err != nil {
		t.Errorf("error: %v", err)
	}

	go func() {
		for err := range errChan {
			t.Errorf("From error channel: %v", err.Err)
		}
	}()

	portal1, portal2 := binder.Subscribe(), binder.Subscribe()
	if v, err := portal1.SendTransforms(
		[]*OTransform{
			&OTransform{
				Position: 6,
				Version:  2,
				Delete:   5,
				Insert:   "universe",
			},
		},
	); v != 2 || err != nil {
		t.Errorf("Send Transform error, v: %v, err: %v", v, err)
	}

	tforms1 := <-portal1.TransformRcvChan
	tforms2 := <-portal2.TransformRcvChan

	if len1, len2 := len(tforms1), len(tforms2); len1 != 1 || len2 != 1 {
		t.Errorf("Wrong count of transforms, tforms1: %v, tforms2: %v", len1, len2)
	}

	portal3 := binder.Subscribe()
	if exp, rec := "hello universe", string(portal3.Document.Content); exp != rec {
		t.Errorf("Wrong content, expected %v, received %v", exp, rec)
	}
}

func badClient(b *BinderPortal, t *testing.T, wg *sync.WaitGroup) {
	// Do nothing, LOLOLOLOLOL AHAHAHAHAHAHAHAHAHA! TIME WASTTTTIIINNNGGGG!!!!
	time.Sleep(50 * time.Millisecond)

	// The first transform is free (buffered chan)
	<-b.TransformRcvChan
	_, open := <-b.TransformRcvChan
	if open {
		t.Errorf("Bad client wasn't rejected")
	}
	wg.Done()
}

func goodClient(b *BinderPortal, t *testing.T, wg *sync.WaitGroup) {
	changes := b.Version + 1
	for change := range b.TransformRcvChan {
		for _, tform := range change {
			if tform.Insert != fmt.Sprintf("%v", changes) {
				t.Errorf("Wrong order of transforms, expected %v, received %v",
					changes, tform.Insert)
			}
			changes++
		}
	}
	wg.Done()
}

func TestClients(t *testing.T) {
	errChan := make(chan BinderError)
	config := DefaultBinderConfig()
	config.FlushPeriod = 5000

	wg := sync.WaitGroup{}

	doc := CreateNewDocument("test", "test1", "hello world")
	binder, err := BindNew(doc, &MemoryStore{documents: map[string]*Document{}}, DefaultBinderConfig(), errChan)
	if err != nil {
		t.Errorf("error: %v", err)
	}

	go func() {
		for err := range errChan {
			t.Errorf("From error channel: %v", err.Err)
		}
	}()

	tform := func(i int) *OTransform {
		return &OTransform{
			Position: 0,
			Version:  i,
			Delete:   0,
			Insert:   fmt.Sprintf("%v", i),
		}
	}

	portal := binder.Subscribe()

	if v, err := portal.SendTransforms(
		[]*OTransform{tform(portal.Version + 1)},
	); v != 2 || err != nil {
		t.Errorf("Send Transform error, v: %v, err: %v", v, err)
	}

	wg.Add(20)

	for i := 0; i < 10; i++ {
		go goodClient(binder.Subscribe(), t, &wg)
		go badClient(binder.Subscribe(), t, &wg)
	}

	wg.Add(50)

	for i := 0; i < 50; i++ {
		vstart := i*3 + 3
		if i%2 == 0 {
			go goodClient(binder.Subscribe(), t, &wg)
			go badClient(binder.Subscribe(), t, &wg)
		}
		if v, err := portal.SendTransforms(
			[]*OTransform{tform(vstart), tform(vstart + 1), tform(vstart + 2)},
		); v != vstart || err != nil {
			t.Errorf("Send Transform error, expected v: %v, got v: %v, err: %v", vstart, v, err)
		}
	}

	binder.Close()

	wg.Wait()
}

type binderStory struct {
	Content    string          `json:"content"`
	Transforms [][]*OTransform `json:"transforms"`
	TCorrected [][]*OTransform `json:"corrected_transforms"`
	Result     string          `json:"result"`
}

type binderStoriesContainer struct {
	Stories []binderStory `json:"binder_stories"`
}

func goodStoryClient(b *BinderPortal, bstory *binderStory, feeds <-chan []*OTransform, t *testing.T) {
	tformIndex, lenCorrected := 0, len(bstory.TCorrected)
	for {
		select {
		case feed := <-feeds:
			b.SendTransforms(feed)
		case ret, open := <-b.TransformRcvChan:
			if !open {
				t.Errorf("channel was closed before receiving last change")
				return
			}
			if lenRcvd, lenCrct := len(ret), len(bstory.TCorrected[tformIndex]); lenRcvd == lenCrct {
				for i, tform := range ret {
					if tform.Version != bstory.TCorrected[tformIndex][i].Version ||
						tform.Insert != bstory.TCorrected[tformIndex][i].Insert ||
						tform.Delete != bstory.TCorrected[tformIndex][i].Delete ||
						tform.Position != bstory.TCorrected[tformIndex][i].Position {
						t.Errorf("Transform not expected, %v != %v", tform, bstory.TCorrected[tformIndex][i])
					}
				}
			} else {
				t.Errorf("Received wrong number of transforms %v != %v", lenRcvd, lenCrct)
			}
			tformIndex++
			if tformIndex == lenCorrected {
				return
			}
		}
	}
}

func TestBinderStories(t *testing.T) {
	nClients := 10

	bytes, err := ioutil.ReadFile("../data/binder_stories.js")
	if err != nil {
		t.Errorf("Read file error: %v", err)
		return
	}

	errChan := make(chan BinderError)
	go func() {
		for err := range errChan {
			t.Errorf("From error channel: %v", err.Err)
		}
	}()

	var scont binderStoriesContainer
	if err := json.Unmarshal(bytes, &scont); err != nil {
		t.Errorf("Story parse error: %v", err)
		return
	}

	for i, story := range scont.Stories {
		doc := CreateNewDocument(fmt.Sprintf("story%v", i), "testing", story.Content)
		binder, err := BindNew(doc, &MemoryStore{documents: map[string]*Document{}}, DefaultBinderConfig(), errChan)
		if err != nil {
			t.Errorf("error: %v", err)
		}

		wg := sync.WaitGroup{}
		wg.Add(nClients)

		feedChan := make(chan []*OTransform, len(story.Transforms))
		for j := 0; j < nClients; j++ {
			go func() {
				goodStoryClient(binder.Subscribe(), &story, feedChan, t)
				wg.Done()
			}()
		}

		for j := 0; j < len(story.Transforms); j++ {
			feedChan <- story.Transforms[j]
		}

		wg.Wait()

		newClient := binder.Subscribe()
		if got, exp := string(newClient.Document.Content), story.Result; got != exp {
			t.Errorf("Wrong result, expected: %v, received: %v", exp, got)
		}
	}
}