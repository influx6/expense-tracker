# Personal Finance Tracker

<!--toc:start-->

- [Personal Finance Tracker](#personal-finance-tracker)
  - [Assumptions](#assumptions)
  - [Tracker Stories and Laws](#tracker-stories-and-laws)
    - [I want to track my overall income](#i-want-to-track-my-overall-income)
      - [Assumptions, Rule, Laws, Invariants](#assumptions-rule-laws-invariants)
    - [I want to be able to be able to track my overall expenses](#i-want-to-be-able-to-be-able-to-track-my-overall-expenses)
      - [Assumptions, Rule, Laws, Invariants](#assumptions-rule-laws-invariants-1)
    - [Load my income (inflows) and expenses (outflows)](#load-my-income-inflows-and-expenses-outflows)
      - [Assumptions, Rule, Laws, Invariants](#assumptions-rule-laws-invariants-2)
  - [CLI Commands](#cli-commands)
    - [Load](#load)
    - [Summarize](#summarize)
    - [Rules](#rules)

<!--toc:end-->

This is a sample application built in Golang to demonstrate the development of an income and expense tracking CLI.

It features a very simple command interface, focused on supporting per user tracking, expense categories, a basic income
tracking

## Assumptions

## Tracker Stories and Laws

### Core Assumptions and Invariants

- [Assumption] All timestamp is in UTC
- [Assumption] The only supported currency is HKD
- [Invariant] All amounts are represented on either side as Decimals and never floats/doubles. No recording or effects on any system of record should ever be done by any other type (go's type system helps enforce this).
- [Rule] All amounts are stored as decimals and never floats/doubles using the [govalues/decimal](https://github.com/govalues/decimal) package.
  [Note:] minor units is an alternative but has edges e.g super large minor units (representing in lowest unit form e.g cents) that scale past u64 boundaries due.

### [Story]: I want to track my overall income

There is only one wallet representative of the asset owned by the user and intialized when they  start using the CLI.

The income is a type of account representative of it's source (salary/employer) and increases the value of the central wallet but also represents a a value coming from another source.

A wallet can also be increased by a incoming transfer from a libability e.g a loan.

[Derived] Wallet represents a reasonable location for which the income is being recorded into for the user.

Wallet should have actions: 
-	`add` to add an amount to the wallet (your asset value increases)
- `deduct` to reduce an amount from the wallet (your asset value decrease).


#### Assumptions, Laws, Invariants

- [Assumption]: The wallet is by default locked to HKD currency and nothing else
- [Law]: You can never add a float or double to the wallet
- [Law]: The wallet may enter negative if negative transactions are recorded before an actual inflow comes in but the rules of double entry must stand:
  SUM(ASSET) + SUM(INCOME) + SUM(LIABILITIES) + SUM(EXPENSES) == 0.
- [Law]: A wallet value increases from two sources: INCOME and TRANSFER_IN libabilities.
- [Law]: Addition to a wallet asset's value is by definition a deduction from the source from which it comes from, ensuring we correctly represent both sides of the entry.

#### Entities, ValueTypes

- [Derived, Entity, Account(Wallet)]: owns all asset records and represent current account value.
- [Derived, ValueType, Decimal(amount)]: the value that can ever be added to a wallet, anything else is disallowed
- [Derived, ValueType, Enum(type)]=income: represent the type of transaction and entry that increases the wallet value
- [Derived, ValueType, Date(when)]: represents the timestamp upon which the wallet was increased (think salary, profits, ...etc).


### [Story]:  I want to be able to be able to track my overall expenses (outflows)

The entity (Me) also wishes to track expense against said [Wallet]. These are deductions from the wallet value to other transaction types, meaning there is a value added to a value type and deducted from the [wallet], once again, keeping the double entry semantics.

#### Assumptions, Rule, Laws, Invariants

- [Assumption]: Each expense has a specific `category` value, representative of the category which connects it to a representation of that expense a a type of account (double entry again).
- [Assumption]: Each expense will always have a `type` of `expense` (that represent it as outgoing, outflows, .etc)
- [Law]: Addition to a expense account type is by definition a deduction from the [Wallet] as source (where value came from), and as a addition to the account for the expense (where value went to), ensuring we correctly represent both sides of the entry.

#### Entities, ValueTypes

- [Derived, ValueType, Enum(typed)]=expense: 
- [Derived, Account(Expense, Category)]: We will represent every category of expense as a type of account within our account, this allows us track the entries against it vs our wallet.
- [Derived, Entity(Transaction)]: represent an event in time against the [wallet]. (has: timestamp, amount, account)


### [Story]:  I have loans that do not exactly fit an expense, as they can temporarily increase cash on hand but are not assets but liabilities and will be repaid, so in essence debts I owe.

It is reasonable to make one adjustment: A +cash inflow could be a loan which is a liability I own, therefore can not be an `expense` (e.g this include loans, transfers from my account to others). 

Instead such types of liabilities are represented as a new type of value type:

- `transfer_in`: movement of value from some other entity defined by said category to us (positive cash or value but its not really an income), even though it increases our [Wallets] value
- `transfer_out`: movement of value from us to back to another entity, which means a reduction of our liability and a transfer out from the [Wallet] balance.

This allows us represent liabilities and their increase or reduction without overloading type=expense.

#### Assumptions, Rule, Laws, Invariants

- [Assumption]: All liabilities are just considered a form of value transfer in either direction.
- [Law]: All liabilities are just a specific type of account that represent that giving lability e.g loan, and its always referenced as (account_type, account_name) where account_type (connects a account type: ASSET (only ever 1), liabilities (transfer_in, transfer_out), EXPENSES, INCOME(represented by type='income')) and account_name the definitive name of the account

### [Story]:  Load my income (inflows) and expenses (outflows)

As a expense tracker CLI, I as the user want to be able to load a csv of incoming and expenses into the application for tracking. I want to be able to load a single entry or multiple entry.

From a batch or multiple input perspective, it should evidently support a csv with the following format:

```csv
when,amount,desc,category,type
```

Where:

- `type` is a 4 set option of: "expense" or "income" or "transfer_in" or "transfer_out"
- `amount` is the value of the spend for that giving item, value is always positive since `type` already represents the direction.
- `desc` is a description of the item
- `category` is the user provided category of the item
- `when` is the relevant datetime of the item

#### Assumptions, Rule, Laws, Invariants

- [Assumption] All timestamp are required to be plain UTC, no attempt is made to do conversion, whilst we can support timestamp with detailed timezone information, lets keep it simple, its all and should be UTC.
- [Decision] Any invalid values stored into a invalid.csv with a portion reported after ingestion with total counts, and indication of the location of the invalid rows so they can review.
- [Law] the amount for a giving row is always positive. Any amount <=0 is considered invalid and skipped as well
- [Law] Invalid rows: type is not our expected enum values, bad digit values, no timestamp, no amounts, will simple be skipped.
  [Note]: We will during load and collected into a bad input csv so users can see what failed loading with a 'reason' column at the end.
- [Law]: `type` can only ever be: `income`, `expense`, `transfer_in`, and `transfer_out`. All other values are considered bad rows.
  [Note:] "expense" - the outgoing expense spent from the core asset wallet/account
  [Note:] "income" - new incoming inflow to the core asset wallet/account
  [Note:] "transfer_in" - are liabilities we take on e.g loan
  [Note:] "transfer_out" - can be considered payment on the those liabilities or us lending out to others - we see them all under the same type

## Models

Overall from the basic stories above, we can identify the following models with some being derived from what we already know from double accounting.

### Model: Entry

A record of a specific movement of a asset or liability value. An entry has a `side` which can only ever be either (`SOURCE` or `DESTINATION`.

- `SOURCE`: where value comes from.
- `DESTINATION`: where value goes to.

An entry is 1 to 1 connected to a single `transaction` (see [Model:Transaction]).

#### Properties

- `entry_id` (UUID): the unique entry for a giving side of a transaction 
- `account_id` (UUID): the unique id of the account this entry is related to (ASSET for income or inflows, liability for loans,..etc, expense related accounts for expenses). 
- Every entry must have an account is leaving or going to based on the `direction`.
- `transaction_id` (UUID): the unique id of the transaction it is related to 
- `direction` (binary enum: `SOURCE` or `DESTINATION`): the identification of where the value is coming from and going to
- `amount` (DECIMAL): the value being moved from `SOURCE` to `DESTINATION`
- `created_at` (timestampz): the timestamp when it was added
- `when` (timestampz): the timestamp of when it happened, always agreeing with a `transactions.when`.

#### Laws

- The `entry.when` can never be different from a `transactions.when`
- The `entry.amount` can never be different from either side of the `direction`, they are locked into place (source.entry.amount == destination.entry.amount)
- Every entry has a `direction` which is either `SOURCE` or `DESTINATION`
- There are only ever two entries per transaction, any count(entries) == `2 < count(entries) > 2` is a bug

### Model: Transaction

A transaction is an event that connects two sides of a move of value where one side represents the SOURCE (source of the outflow) and DESTINATION (destination of the inflow) being represented.

Every record of inflows, and outflows are simply a representation of value moving between accounts (see [Model: Account](#model:account)) recorded in the 2 entry a transaction has. A specific transaction can never have more than 2 entries and never have only 1 or 0 entries.

And the entries specifically indicate what provides the value moved from the [Account] source to the destination [Account].

#### Laws

- A transaction has always two types of entries each with a specific `direction`:(1 `DESTINATION`, 1 `SOURCE`)
- A transaction is related to two accounts.
- A transaction entries must always be equal on both sides of the entries.
- A transaction entry.amount on both must match and one side must be SOURCE and the other DESTINATION.
- A transaction can never have entries of the same `direction`.
- A transaction represent an event in time for a specific `category`

#### Properties

- transaction_id: (UUID) representation of the specifi transaction that is being performed.
- when: (timestampz) representation of `when` said transaction occured
- user_id: (UUID) relevant user which transaction is related to (we really only ever have one)

### Model: Account

The system has a series of Accounts representing the different sets of system of records for a giving category of asset or liability. It can only ever be a set of:

#### Properties

- `user_id`: (UUID) relevant user who owns the account (we really only ever have one)
- `account_type`: INCOME (your salary or other inflow sources), EXPENSE (outflow sources), LABILITIES (considered what you owe or are owed), ASSET (singular, the wallet that represent your overall account balance)
- `account_name`: the relevant user friendly name we wish to call the accunt, generally its the category name which represent the type of asset/liability being represented (think: income, loans, leasure, etc)
- `account_id`: singular, unique ID representing the account for relationship mapping
- Key:
  - UNIQUE(account_name, account_type): uniquely represent a specific account to ensure no two accounts can represent the smae thing.
- Index:
  - UNIQUE INDEX(account_name, account_type): uniquely represent a specific account to ensure no two accounts can represent the smae thing.
  
#### Laws

- Every account has a specific `account_type` (ASSET, EXPENSE, LIABILITY, INCOME)
- Every account except the ASSET account is user generated and always from the incoming transaction log being ingested
- Every accounts sum(account.inflows) - sum(accounts.outflow) tells us the accounts left over balance.


## CLI Commands

### Load

Loads a giving provided CSV file into the tracker application. Internally we process, process and store the facts into a embedded sqlite database table
and entirely use that to query and provide relevant expense reports.

### Summarize

Returns a summarized month by month report of existing spending showing total income, expenses, expenses by categories per year, per month

```
tracker summarize
```

Get a specific month's tracking summary for a giving year:

```bash
tracker summarize --year 2026 --month jan|january|01|1
```

### Rules

Allows executing a specific rule implementation for a all periods or a specific time period (year and month required) to identify a specific
sets of transactions that match specific criterias.

Rules can be easily added in into [rules][./rules] and registers it with the [rules](./cmd/rules) command:
cmd/rules.go

```go
RULES = []Rule {
    ...,
    MY_RULE,
}
```

Then you can run your new rule:

```bash
tracker rule MY_RULE
tracker rule MY_RULE --year 2026
tracker rule MY_RULE --year 2026 --month jan|january|01|1
```
