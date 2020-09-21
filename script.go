package main

import (
	"io"
	"net/http"
)

func script(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript")
	io.WriteString(w, `"use strict"
let AtomKV = {
	BASE: new URL(document.currentScript.src).origin,

	get: function(key) {
		let url = this.BASE + key
		return new Promise(function(resolve, reject) {
			let xhr = new XMLHttpRequest()
			xhr.onload = function() {
				if (xhr.status == 404) {
					resolve([undefined, -1])
				} else {
					let revision = Number(xhr.getResponseHeader('X-Revision'))
					resolve([JSON.parse(xhr.responseText), revision])
				}
			}
			xhr.onerror = function() {
				reject(xhr.responseText)
			}
			xhr.open('GET', url, true)
			xhr.send()
		})
	},

	set: function(key, value) {
		let url = this.BASE + key
		return new Promise(function(resolve, reject) {
			let xhr = new XMLHttpRequest()
			xhr.onload = function() {
				resolve()
			}
			xhr.onerror = function() {
				reject(xhr.responseText)
			}
			xhr.open('POST', url, true)
			xhr.send(JSON.stringify(value))
		})
	},

	update: function(key, value, revision) {
		let url = this.BASE + key
		return new Promise(function(resolve, reject) {
			let xhr = new XMLHttpRequest()
			xhr.onload = function() {
				resolve(xhr.status == 200)
			}
			xhr.onerror = function() {
				reject(xhr.responseText)
			}
			xhr.open('PUT', url, true)
			xhr.setRequestHeader('X-Revision', String(revision))
			xhr.send(JSON.stringify(value))
		})
	},

	subscribe: async function*(keypath) {
		let resolve = null
		let promise = null
		function reset() {
			promise = new Promise(function(r) {resolve = r})
		}
		reset()
		let sse = new EventSource(this.BASE + keypath)
		sse.onmessage = function(event) {
			let value = JSON.parse(event.data)
			let [key, revision] = event.lastEventId.split(/:/)
			resolve([key, value, Number(revision)])
			reset()
		}
		try {
			for (;;) {
				yield await promise
			}
		} finally {
			sse.close()
		}
	}
}`)
}
