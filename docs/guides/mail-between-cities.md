---
title: "Mail Between Cities"
description: Let agents in one city mail agents in another with gc mail send agent@city.
---

An agent can mail an agent in another city by addressing it as
`<agent>@<city>`:

```bash
gc mail send jane@ansible -s "Deploy done" -m "v1.4 is live" --notify
```

The message is created in the `ansible` city, from `<sender>@<this city>`, so
`jane` sees who wrote and `gc mail reply` answers in the sender's city. A read
copy stays in the sending city, in the same thread, so `gc mail thread` shows
the whole conversation on both sides. A bare name, or `<agent>@<this city>`,
still means an agent here. Mail carries body text only.

## Set up a pair of cities

Each city needs two things for each peer: a way to reach it, and the secret the
two share.

1. **Reach.** The peer's address comes from a [gc context](/reference/cli#gc-context)
   named after the peer city. Add one on every host that sends to it:

   ```bash
   gc context add ansible --url https://ansible.example.net
   ```

   The context's TLS, credential, and `grant_command` settings apply to the
   mail write as they do to any other remote command.

2. **Identity.** Put the same random secret in a file on both hosts, and
   declare the peer in each city's `city.toml`:

   ```toml
   # wonderland's city.toml
   [[mail.peer]]
   city = "ansible"
   token_file = "/etc/gascity/mail-peers/ansible-wonderland.token"
   ```

   ```toml
   # ansible's city.toml
   [[mail.peer]]
   city = "wonderland"
   token_file = "/etc/gascity/mail-peers/ansible-wonderland.token"
   ```

   A city sends the token with mail to the peer, and accepts mail from an
   `<agent>@<peer>` sender only when the token matches. A caller without it
   cannot send as one of the peer's agents. A relative `token_file` resolves
   against the city directory.

The token proves which city a message came from, not which agent in it: any
holder of the `ansible` token can send as any `<agent>@ansible`.

## Teach agents the address form

Add one line to each city's `AGENTS.md` (or the prompt your agents share):

```text
Mail an agent in another city with `gc mail send <agent>@<city>`; reply to such mail with `gc mail reply <id>`.
```
