# AtomKV

AtomKV is an in-memory, JSON, key-value service with compare-and-swap
updates and [event streams][es]. It supports [CORS][cors] and hosts its
own JavaScript library, so any page on any domain can use it seamlessly.

Keys are hierarchical, always begin with a slash, and never end with a
slash. Components between slashes are limited to letters, numbers, dash,
and underscore. Listeners can monitor a specific key or all keys rooted
under a key space. Each key-value has a revision number which starts at
zero and increments by one for each update.

## Usage

    $ go build
    $ ./atomkv -addr :8000

## JavaScript API (high level)

The JavaScript library is hosted at `/` and defines an `AtomKV` object
with the following functions (with TypeScript-like notation):

```ts
/**
 * Retrieve the current value for the given key, returning the value and
 * the revision number. If the key does not exist, returns [null, -1].
 */
AtomKV.get = async function(key: string): [any, number]

/**
 * Forcefully assign a key to a JSON-serializable value.
 */
AtomKV.set = async function(key: string, value: any)

/**
 * Attempt to update the value for the specific key, but only if it 
 * would have the given revision number. Returns true if successful,
 * otherwise false. On failure, use .get() to retrieve the current value
 * and revision, then try again.
 */
AtomKV.update = async function(key: string, value: any, revision: number): boolean

/**
 * Returns an asynchronous generator of update events for the given key
 * path. If the key path ends with a slash, it will monitor the entire
 * hierarchy under that path. Each yield returns the key, value, and
 * revision number. Typically used in a "for await" statement.
 */
AtomKV.subscribe = async function*(keypath: string): [string, any, number]
```

### JavaScript examples

The following example iterates the Fibonacci state at `/fib`. This is
safe for multiple concurrent clients because of the compare-and-swap
semantics of `.update()`. If there's a conflict, the fastest client
succeeds and the others retry.

```js
async function init() {
    // Acceptably fails if already initialized
    AtomKV.update('/fib', [0, 1], 0)
}

async function increment() {
    for (;;) {
        let [[a, b], revision] = await AtomKV.get('/fib')
        let next = [b, a + b]
        if (await AtomKV.update('/fib', next, revision + 1)) {
            return next  // success
        }
    }
}
```

This example monitors all key-values under a given "session" ID:

```js
async function monitor(session) {
    for await (let [key, value, rev] of AtomKV.subscribe(`/${session}/`)) {
        console.log(key, value, rev)
    }
}

let SESSION_ID = Math.floor(Math.random() * 0xffffffff).toString(16)
console.log(`SESSIONID = ${SESSION_ID}`)
monitor(SESSION_ID)
```

## HTTP API (low level)

There's really just one endpoint, `/`, which responds differently to
different methods.

### `GET /`

Serves the JavaScript library documented above. Include this on your
page using a `script` tag. 

### `GET /path/to/key`

Returns the JSON-encoded value for the given key. The `X-Revision`
header indicates the revision number.

### `GET /path/to/key` with `Accept: text/event-stream`

If the `Accept` header indicates `text/event-stream` then the request
will use [Server-sent Events][sse]. If the path ends in a slash, all
keys under that path are monitored. It is not possible to monitor `/`.

The `data` field for each event is the JSON-encoded value, and the `id`
field is `/path/to/key:revision`. Keys cannot contain a colon, so this
is trivial to parse.

### `POST /path/to/key`

Forcefully overwrite the key with a JSON-encoded value. This always
succeeds for valid keys.

### `PUT /path/to/key`

Attempt to store a new JSON-encoded value under the given key. An
`X-Revision` header *must* be supplied, indicating the assumed new
revision number — one more than the last observed revision. It only
succeeds if the expected revision number matches.


[cors]: https://developer.mozilla.org/en-US/docs/Glossary/CORS
[es]: https://developer.mozilla.org/en-US/docs/Web/API/EventSource
[sse]: https://html.spec.whatwg.org/multipage/server-sent-events.html
