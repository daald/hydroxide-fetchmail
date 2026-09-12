package exports

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/emersion/go-mbox"
	"github.com/emersion/go-message"
	"github.com/emersion/go-message/mail"
	"github.com/emersion/go-message/textproto"

	"github.com/emersion/hydroxide/protonmail"
)

func WriteMessage(c *protonmail.Client, privateKeys openpgp.KeyRing, w io.Writer, msg *protonmail.Message) error {
	mimeType := msg.MIMEType
	if mimeType == "" {
		mimeType = "text/html"
	}

	br := bufio.NewReader(strings.NewReader(msg.Header))
	th, err := textproto.ReadHeader(br)
	if err != nil {
		return fmt.Errorf("failed to read message header: %v", err)
	}

	mh := mail.Header{Header: message.Header{Header: th}}

	md, err := msg.Read(privateKeys, nil)
	if err != nil {
		return err
	}

	if len(msg.Attachments) == 0 {
		mh.SetContentType(mimeType, map[string]string{"charset": "utf-8"})
		mh.Set("Content-Transfer-Encoding", "quoted-printable")

		mw, err := mail.CreateSingleInlineWriter(w, mh)
		if err != nil {
			return fmt.Errorf("failed to create message writer: %v", err)
		}

		// TODO: check signature
		if _, err := io.Copy(mw, md.UnverifiedBody); err != nil {
			return err
		}

		return mw.Close()
	}

	mw, err := mail.CreateWriter(w, mh)
	if err != nil {
		return fmt.Errorf("failed to create message writer: %v", err)
	}

	var ih mail.InlineHeader
	ih.SetContentType(mimeType, map[string]string{"charset": "utf-8"})
	ih.Set("Content-Transfer-Encoding", "quoted-printable")

	iw, err := mw.CreateSingleInline(ih)
	if err != nil {
		return fmt.Errorf("failed to create inline writer: %v", err)
	}
	// TODO: check signature
	if _, err := io.Copy(iw, md.UnverifiedBody); err != nil {
		return err
	}
	if err := iw.Close(); err != nil {
		return err
	}

	for _, att := range msg.Attachments {
		if err := writeAttachment(c, privateKeys, mw, att); err != nil {
			return fmt.Errorf("failed to write attachment %q: %v", att.Name, err)
		}
	}

	return mw.Close()
}

func writeAttachment(c *protonmail.Client, privateKeys openpgp.KeyRing, mw *mail.Writer, att *protonmail.Attachment) error {
	rc, err := c.GetAttachment(att.ID)
	if err != nil {
		return fmt.Errorf("failed to download attachment: %v", err)
	}
	defer rc.Close()

	md, err := att.Read(rc, privateKeys, nil)
	if err != nil {
		return fmt.Errorf("failed to decrypt attachment: %v", err)
	}

	var ah mail.AttachmentHeader
	ah.SetContentType(att.MIMEType, nil)
	ah.Set("Content-Transfer-Encoding", "base64")
	ah.SetFilename(att.Name)
	if att.ContentID != "" {
		ah.Set("Content-Id", att.ContentID)
	}

	aw, err := mw.CreateAttachment(ah)
	if err != nil {
		return err
	}

	// TODO: check signature
	if _, err := io.Copy(aw, md.UnverifiedBody); err != nil {
		return err
	}
	return aw.Close()
}

func ExportMessage(c *protonmail.Client, privateKeys openpgp.KeyRing, w io.Writer, id string) error {
	msg, err := c.GetMessage(id)
	if err != nil {
		return fmt.Errorf("failed to fetch message: %v", err)
	}

	return WriteMessage(c, privateKeys, w, msg)
}

func ExportMessageMbox(c *protonmail.Client, privateKeys openpgp.KeyRing, mbox *mbox.Writer, id string) error {
	msg, err := c.GetMessage(id)
	if err != nil {
		return fmt.Errorf("failed to fetch message: %v", err)
	}

	w, err := mbox.CreateMessage(msg.Sender.Address, msg.Time.Time())
	if err != nil {
		return fmt.Errorf("failed to create mbox message: %v", err)
	}

	return WriteMessage(c, privateKeys, w, msg)
}

func ExportConversationMbox(c *protonmail.Client, privateKeys openpgp.KeyRing, mbox *mbox.Writer, id string) error {
	_, msgs, err := c.GetConversation(id, "")
	if err != nil {
		return fmt.Errorf("failed to fetch conversation: %v", err)
	}

	for _, msg := range msgs {
		if err := ExportMessageMbox(c, privateKeys, mbox, msg.ID); err != nil {
			return fmt.Errorf("failed to export conversation message: %v", err)
		}
	}

	return nil
}
