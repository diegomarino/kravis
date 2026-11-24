# Getting the Message-ID of an email

Record the Message-ID of every email as `email:message:<message-id>`, without the angle brackets. It is the same in every mailbox that holds the message, so the same email recorded twice, by two agents, is one interaction. The provider's own IDs are extra references and never identify.

Example: a message with the header `Message-ID: <CAF3x9=ab12@example.com>` is

```text
--ref email:message:CAF3x9=ab12@example.com
```

## Gmail

- Ask for the message with the `RAW` format. The other formats may leave out the headers you need.
- Find the `Message-ID` header. A long one is folded over several lines: join the lines, remove the line breaks and the spaces that start them, and drop the `<` and `>`.
- Record the message and its thread with the account, next to the Message-ID: `gmail:message:<id>@<account>` and `gmail:thread:<id>@<account>`.

```text
--ref email:message:CAF3x9=ab12@example.com --ref gmail:message:18f3a2c9d1e4b567@ana@example.com --ref gmail:thread:18f3a2c9d1e4b000@ana@example.com
```

## Outlook (Microsoft Graph)

- Ask for `$select=internetMessageId`. Do it when you list messages too, and for sent mail: the field is not part of the default answer.
- `internetMessageId` is the Message-ID: drop the `<` and `>`.
- The message `id` goes as `outlook:message:<id>@<account>`. It never identifies, because it changes when the mail moves to another folder.

```text
--ref email:message:CAF3x9=ab12@example.com --ref outlook:message:AAMkAGI2@ana@example.com
```

## IMAP and Apple Mail

Read the `Message-ID` header of the message and drop the `<` and `>`. A message UID is not a Message-ID: it belongs to one folder.

## There is no Message-ID

Record only the provider's reference, for example `--ref gmail:message:18f3a2c9d1e4b567@ana@example.com`. Kravis cannot tell that the same email arrives twice, so it does not deduplicate: look for the interaction yourself before you create it again.
