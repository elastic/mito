# Mito

(Mito is experimental and subject to change).

Mito is a sketch for a message stream processing engine based on [CEL](https://github.com/google/cel-go). Mito provides tools in the lib directory to support collection processing, timestamp handling and other common tasks (see test snippets in [testdata](./testdata) and docs at https://godocs.io/github.com/elastic/mito/lib).

Some features of mito depend on features that do not yet exist in mainline CEL and some are firmly within the realms of dark arts.

The `mito` command will apply CEL expressions to a JSON value input under the label `state` within the CEL environment. This is intended to be used as a debugging and playground tool.

For example the following CEL expression processes the stream below generating the Cartesian product of the `num` and `let` fields and retaining the original message and adding timestamp metadata.

```
state.map(e, has(e.other) && e.other != '',
	has(e.num) && size(e.num) != 0 && has(e.let) && size(e.let) != 0 ?
		// Handle Cartesian product.
		e.num.map(v1,
			e.let.map(v2,
				e.with({
					"@triggered": now,   // As a value, the start time.
					"@timestamp": now(), // As a function, the time the action happened.
					"original": e.encode_json(),
					"numlet": e.num+e.let,
					"num": v1,
					"let": v2,
				})
		))
	:
		// Handle cases where there is only one of num or let and so
		// the Cartesian product would be empty: S × Ø, S = num or let.
		//
		// This expression is nested to agree with the Cartesian
		// product (an alternative is to flatten that for each e).
		[[e.with({
			"@triggered": now,   // As a value, the start time.
			"@timestamp": now(), // As a function, the time the action happened.
			"original": e.encode_json(),
		})]] 
).flatten().drop_empty().as(res,
	{
		"events": res,
		// Get cursor summary.
		"cursor": res.collate('@timestamp').as(t, {"timestamps":{
			"first": t.min(),
			"last": t.max(),
			"list": t,
		}}),
	}
)
```
working on
```json
[
	{
		"let": ["a", "b"],
		"num": ["1", "2"],
		"other": "random information for first"
	},
	{
		"let": ["aa", "bb"],
		"num": ["12", "22", "33"],
		"other": "random information for second"
	},
	{
		"let": ["a", "b"],
		"num": [],
		"other": "random information for third"
	},
	{
		"let": [], 
		"num": ["1", "2"],
		"other": "random information for fourth"
	},
	{
		"num": ["1", "2"],
		"other": "random information for fifth"
	},
	{
		"let": ["y", "z"],
		"num": ["-1", "-2", "-3"]
	}
]
```
gives
```json
{
	"cursor": {
		"timestamps": {
			"first": "2022-04-04T04:28:00.36378Z",
			"last": "2022-04-04T04:28:00.364034Z",
			"list": [
				"2022-04-04T04:28:00.36378Z",
				"2022-04-04T04:28:00.36385Z",
				"2022-04-04T04:28:00.363863Z",
				"2022-04-04T04:28:00.363872Z",
				"2022-04-04T04:28:00.363885Z",
				"2022-04-04T04:28:00.363893Z",
				"2022-04-04T04:28:00.363947Z",
				"2022-04-04T04:28:00.363955Z",
				"2022-04-04T04:28:00.363981Z",
				"2022-04-04T04:28:00.364003Z",
				"2022-04-04T04:28:00.364012Z",
				"2022-04-04T04:28:00.364021Z",
				"2022-04-04T04:28:00.364034Z"
			]
		}
	},
	"events": [
		{
			"@timestamp": "2022-04-04T04:28:00.36378Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "a",
			"num": "1",
			"numlet": [
				"1",
				"2",
				"a",
				"b"
			],
			"original": "{\"let\":[\"a\",\"b\"],\"num\":[\"1\",\"2\"],\"other\":\"random information for first\"}",
			"other": "random information for first"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.36385Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "b",
			"num": "1",
			"numlet": [
				"1",
				"2",
				"a",
				"b"
			],
			"original": "{\"let\":[\"a\",\"b\"],\"num\":[\"1\",\"2\"],\"other\":\"random information for first\"}",
			"other": "random information for first"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.363863Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "a",
			"num": "2",
			"numlet": [
				"1",
				"2",
				"a",
				"b"
			],
			"original": "{\"let\":[\"a\",\"b\"],\"num\":[\"1\",\"2\"],\"other\":\"random information for first\"}",
			"other": "random information for first"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.363872Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "b",
			"num": "2",
			"numlet": [
				"1",
				"2",
				"a",
				"b"
			],
			"original": "{\"let\":[\"a\",\"b\"],\"num\":[\"1\",\"2\"],\"other\":\"random information for first\"}",
			"other": "random information for first"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.363885Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "aa",
			"num": "12",
			"numlet": [
				"12",
				"22",
				"33",
				"aa",
				"bb"
			],
			"original": "{\"let\":[\"aa\",\"bb\"],\"num\":[\"12\",\"22\",\"33\"],\"other\":\"random information for second\"}",
			"other": "random information for second"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.363893Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "bb",
			"num": "12",
			"numlet": [
				"12",
				"22",
				"33",
				"aa",
				"bb"
			],
			"original": "{\"let\":[\"aa\",\"bb\"],\"num\":[\"12\",\"22\",\"33\"],\"other\":\"random information for second\"}",
			"other": "random information for second"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.363947Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "aa",
			"num": "22",
			"numlet": [
				"12",
				"22",
				"33",
				"aa",
				"bb"
			],
			"original": "{\"let\":[\"aa\",\"bb\"],\"num\":[\"12\",\"22\",\"33\"],\"other\":\"random information for second\"}",
			"other": "random information for second"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.363955Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "bb",
			"num": "22",
			"numlet": [
				"12",
				"22",
				"33",
				"aa",
				"bb"
			],
			"original": "{\"let\":[\"aa\",\"bb\"],\"num\":[\"12\",\"22\",\"33\"],\"other\":\"random information for second\"}",
			"other": "random information for second"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.363981Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "aa",
			"num": "33",
			"numlet": [
				"12",
				"22",
				"33",
				"aa",
				"bb"
			],
			"original": "{\"let\":[\"aa\",\"bb\"],\"num\":[\"12\",\"22\",\"33\"],\"other\":\"random information for second\"}",
			"other": "random information for second"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.364003Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": "bb",
			"num": "33",
			"numlet": [
				"12",
				"22",
				"33",
				"aa",
				"bb"
			],
			"original": "{\"let\":[\"aa\",\"bb\"],\"num\":[\"12\",\"22\",\"33\"],\"other\":\"random information for second\"}",
			"other": "random information for second"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.364012Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"let": [
				"a",
				"b"
			],
			"original": "{\"let\":[\"a\",\"b\"],\"num\":[],\"other\":\"random information for third\"}",
			"other": "random information for third"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.364021Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"num": [
				"1",
				"2"
			],
			"original": "{\"let\":[],\"num\":[\"1\",\"2\"],\"other\":\"random information for fourth\"}",
			"other": "random information for fourth"
		},
		{
			"@timestamp": "2022-04-04T04:28:00.364034Z",
			"@triggered": "2022-04-04T04:28:00.363778Z",
			"num": [
				"1",
				"2"
			],
			"original": "{\"num\":[\"1\",\"2\"],\"other\":\"random information for fifth\"}",
			"other": "random information for fifth"
		}
	]
}
```

(Run `mito -data example.json example.cel` to see this locally.)