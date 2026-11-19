# Personal Finance Tracker

<!--toc:start-->

- [Personal Finance Tracker](#personal-finance-tracker)
  - [Assumptions](#assumptions)
  - [Tracker Stories and Laws](#tracker-stories-and-laws)
    - [Core Assumptions and Invariants](#core-assumptions-and-invariants)
    - [[Story]: I want to track my overall income](#story-i-want-to-track-my-overall-income)
      - [Entities, ValueTypes](#entities-valuetypes)
        - [[Entity, Wallet([Account(Asset),...])]](#entity-walletaccountasset)
          - [Wallet Actions](#wallet-actions)
        - [[Entity, Account(account_type=ASSET|INCOME|EXPENSE|LIABILITY)]](#entity-accountaccounttypeassetincomeexpenseliability)
          - [Actions](#actions)
      - [Assumptions, Laws, Invariants](#assumptions-laws-invariants)
    - [[Story]: I want to be able to track all my expenses (outflows)](#story-i-want-to-be-able-to-track-all-my-expenses-outflows)
      - [Entities, ValueTypes](#entities-valuetypes-1)
      - [Assumptions, Rule, Laws, Invariants](#assumptions-rule-laws-invariants)
    - [[Story]: I have loans that do not exactly fit an expense, as they can temporarily increase cash on hand but are not assets but liability and will be repaid, so in essence debts I owe.](#story-i-have-loans-that-do-not-exactly-fit-an-expense-as-they-can-temporarily-increase-cash-on-hand-but-are-not-assets-but-liability-and-will-be-repaid-so-in-essence-debts-i-owe)
      - [Entities, ValueTypes](#entities-valuetypes-2)
      - [Assumptions, Rule, Laws, Invariants](#assumptions-rule-laws-invariants-1)
    - [[Story]: Load my income (inflows) and expenses (outflows)](#story-load-my-income-inflows-and-expenses-outflows)
      - [Assumptions, Rule, Laws, Invariants](#assumptions-rule-laws-invariants-2)
  - [Models](#models)
    - [Model: Entry](#model-entry)
      - [Properties](#properties)
      - [Laws](#laws)
    - [Model: Transaction](#model-transaction)
      - [Laws](#laws-1)
      - [Properties](#properties-1)
    - [Model: Account](#model-account)
      - [Properties](#properties-2)
      - [Laws](#laws-2)
  - [CLI Commands](#cli-commands)
    - [Load](#load)
    - [Summarize](#summarize)
    - [Rules](#rules)

<!--toc:end-->

This is a sample application built in Golang to demonstrate the development of an income and expense tracking CLI.

It features a very simple command interface, focused on supporting per user tracking, expense categories, a basic income
tracking

## AI Disclaimer

I used AI where it made sense selectively, such as:

- Writing tests for established code and edgecases
- Speeding through guided implementation steps

## Important

1. The tests are kept as simple as possible, and I selected to use AI to generate these, its more labour that actual work since the core is the code and more so AI has its uses in writing tests to validate behaviour after the underlying rules, invariants and specification for the expected behaviour is specified.

## Assumptions

## Tracker Stories and Laws

### Core Assumptions and Invariants

- [Assumption] All timestamp is in UTC
- [Assumption] The only supported currency is HKD
- [Invariant] All amounts are represented on either side as Decimals and never floats/doubles. No recording or effects on any system of record should ever be done by any other type (go's type system helps enforce this).
- [Rule] All amounts are stored as decimals and never floats/doubles using the [govalues/decimal](https://github.com/govalues/decimal) package and we use [Decimal.parse] for strings.
  [Note:] minor units is an alternative but has edges e.g super large minor units (representing in lowest unit form e.g cents) that scale past u64 boundaries e.g due to multiplication would go past the limits a u64 can hold.
- [Law] All amounts must be actual currency value, exponents are not allowed.
- [Law] All amounts are expected to be in HKD, so max 2 decimal places for cents, never more.

### [Story]: I want to track my overall income

There is only one wallet representative of the asset owned by the user and initialized when they start using the CLI.

Income/Inflows are a type of account representative of it's source (e.g salary/employer) and increases the value of the central wallet but also represents a value coming from another source.

A wallet can also be increased by a incoming transfer from a libability e.g a loan, credit card.

[Derived] Wallet represents a reasonable location for which the income is being recorded into for the user.

#### Entities, ValueTypes

- [Derived, Entity, Wallet(Account(acount_type=ASSET)]: owns all asset records and represent current account value.
- [Derived, ValueType, Decimal(amount)]: the value that can ever be added to a wallet, anything else is disallowed
- [Derived, Entity, Account(acount_type=INCOME)]: account created to represent the type of income, created as per whatever the user reports as ValueType(type=income).
- [Derived, ValueType, Enum(type)]=income: represent the type of transaction and entry that increases the wallet value
- [Derived, ValueType, Date(when)]: represents the timestamp upon which the wallet was increased (think salary, profits, ...etc).
- [Derived, Event(TransactionEvent)]: represent an event in time against the [wallet] for the ASSET account and the account for the category. (has: timestamp, amount, category, type=income)

##### [Entity, Wallet([Account(Asset),...])]

The central owner and system of record for a user's asset is a Wallet. It both records addition and deduction to the asset account, it records the central underlying asset owned by the user.

- Who owns and calls it? - The cli
- How many writers to it? - Only one, the CLI surface (writes must be sequential to it)
- How many exists? - Only 1 (there is only ever 1 ASSET account type per user)
- How long does it exists ? - Forever has long as the CLI and user (we really only ever have 1) exists

###### Wallet Actions

The wallet has only one action: `apply([Wallet]) error`:

It takes one or more `TransactionEvent(when, type, amount, category, desc)` and applies the events to the wallet account.

##### [Entity, Account(account_type=ASSET|INCOME|EXPENSE|LIABILITY)]

The central means of representing the event against a giving type of asset or liability (its used by the wallet for the precreated asset account maintaining the users running balance). Its sole purpose is to relationship point defining the giving expense type.

- Who owns and calls it? - The wallet
- How many writers to it? - The wallet writes all accounts and their effects
- How many exists? - As many as the unique categories that user reports + 1 predefined ASSET account for the recording of asset balance for the user
- How long does it exists ? - Forever has long as the CLI and user.

###### Actions

No actions, all actions are controlled by the [Wallet] which defines what is registered on to what account.

#### Assumptions, Laws, Invariants

- [Assumption]: The wallet asset account is by default locked to HKD currency and nothing else
- [Law]: All account types are referenced and keyed by (account_type, account_name) which must always be unique (enforced always)
- [Law]: An account's type is it connected to a set enum (ASSET|INCOME|EXPENSE|LIABILITY)
- [Law]: An account's name is a limited set of user defined categories (all alphabets, no special symbols, enforced except underscore `_`).
- [Law]: You can never add a float or double to the wallet
- [Law]: The wallet may enter negative if negative transactions are recorded before an actual inflow comes in but the rules of double entry must stand:
  SUM(ASSET) + SUM(INCOME) + SUM(LIABILITY) + SUM(EXPENSES) == 0.
- [Law]: A wallet asset count value increases from two sources: INCOME and TRANSFER_IN libabilities.
- [Law]: Addition to a wallet asset's value is by definition a deduction from the source from which it comes from, ensuring we correctly represent both sides of the entry.

### [Story]: I want to be able to track all my expenses (outflows)

The entity (Me) also wishes to track expense against said [Wallet]. These are deductions from the wallet asset value to other transaction types, meaning there is a value added to a value type and deducted from the [wallet, Account(asset)], once again, keeping the double entry semantics.

#### Entities, ValueTypes

- [Derived, ValueType, Enum(typed)]=expense:
- [Derived, Account(acount_type=EXPENSE)]: We will represent every category of expense as a type of account(account_type=EXPENSE) and category as account(account_name=categorty), representing that giving event.
- [Derived, Event(TransactionEvent)]: represent an event in time against the [wallet] for the ASSET account and the account for the category. (has: timestamp, amount, category, type=expense)

#### Assumptions, Rule, Laws, Invariants

- [Assumption]: Each expense has a specific `category` value which connects as the `Account.account_name` (lowercased, no numbers, only letters +`_` allowed) and it's type connects to the `Account.account_type`, representative of the category which connects it to a representation of that expense a a type of account (double entry again).
- [Assumption]: Each expense will always have a `type` of `expense` (that represent it as outgoing, outflows, .etc)
- [Law]: Addition to a expense account type is by definition a deduction from the [Wallet] as source (where value came from), and as a addition to the account for the expense (where value went to), ensuring we correctly represent both sides of the entry.

### [Story]: I have loans that do not exactly fit an expense, as they can temporarily increase cash on hand but are not assets but liability and will be repaid, so in essence debts I owe.

It is reasonable to make one adjustment: An addition to the asset could could infact be a loan, credit card debt which is a liability I own, therefore can not be an `expense` (paid once or a subscription, consumed item) (e.g this include loans, transfers to me by others to be repaid).

Instead such types of liability are represented as two new type of event (not `expense`):

- `transfer_in`: movement of value from some other entity defined by said category to us (positive cash or value but its not really an income), even though it increases our [Wallets] value
- `transfer_out`: movement of value from us back to another entity, which means a reduction of our liability and a transfer out from the [Wallet, Account(ASSET)] balance.

This allows us represent liability and their increase or reduction without overloading type=expense.

#### Entities, ValueTypes

- [Derived, ValueType, Enum(typed)]=(transfer_in, transfer_out):
- [Derived, Account(acount_type=LIABILITY)]: accounts for these types of events have the account_type=LIABILITY, the processing of said entries for the account based on the direction defines if its a increase of liability or the reduction of it.
- [Derived, Event(TransactionEvent)]: represent an event in time against the [wallet] for the ASSET account and the account for the category. (has: timestamp, amount, category, type=transfer_in|transfer_out) and its `type` defines the the direction of value movement (transfer_in adds to the ASSET value, transfer_out removes from the ASSET value and reduction of liability).

#### Assumptions, Rule, Laws, Invariants

- [Assumption]: All liability are just considered a form of value transfer in either direction.
- [Law]: A liability under `transfer_in` adds value to the Account(account_type=ASSET)
- [Law]: A liability under `transfer_out` removes value to the Account(account_type=ASSET)
- [Law]: Summation of all entries under `account_type=LIABILITY` tells us the current owned liability for a giving user as well as the net effect those have on the account(account_type=ASSET) balance.

### [Story]: Load my income (inflows) and expenses (outflows)

As a expense tracker CLI, I as the user want to be able to load a csv of incoming and expenses and liability into the application for tracking. I want to be able to load a single entry or multiple entry.

From a single entry perspective: I should just be able to specify:

```bash
cli record --when [Timestamp] --amount [AMOUNT] --desc [DESC] --category [category] --type=[enum(expense,income,transfer_in,transfer_out)]
```

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
- [Law] Invalid rows: type is not our expected enum values or not type, bad digit values, no timestamp, no amounts, will simple be skipped.
  [Note]: We will during load and collected into a bad input csv so users can see what failed loading with a 'reason' column at the end.
- [Law] A batch is loaded in a transaction and the wallet roles up the batch to validate double entry requirement matches sum(asset + income + liability + expenses) == 0 on every load, any deviating is revoked to keep wallet in a consistent, correct state.
- [Law]: `type` can only ever be: `income`, `expense`, `transfer_in`, and `transfer_out`. All other values are considered bad rows.
  [Note:] "expense" - the outgoing expense spent from the core asset wallet/account
  [Note:] "income" - new incoming inflow to the core asset wallet/account
  [Note:] "transfer_in" - are liability we take on e.g loan
  [Note:] "transfer_out" - can be considered payment on the those liability or us lending out to others - we see them all under the same type

## Models

Overall from the basic stories above, we can identify the following models with some being derived from what we already know from double accounting.

### Model: Entry

A record of a specific movement of a asset or liability value. An entry has a `side` which can only ever be either (`SOURCE` or `DESTINATION`). No outsider system ever generates or submits an entry, its internal.

- `SOURCE`: where value comes from.
- `DESTINATION`: where value goes to.

An entry is 1 to 1 connected to a single `transaction` (see [Model:Transaction]).

#### Properties

- `entry_id` (UUID): the unique entry for a giving side of a transaction
- `account_id` (UUID): the unique id of the account this entry is related to (asset for asset increases, income for income inflows (deduct from an income account) into asset, liability for loans,..etc, expense related accounts for expenses).
- Every entry must have an account is leaving or going to based on the `direction`.
- `transaction_id` (UUID): the unique id of the transaction it is related to
- `direction` (binary enum: `SOURCE` or `DESTINATION`): the identification of where the value is coming from and going to
- `amount` (DECIMAL): the value being moved from `SOURCE` to `DESTINATION`
- `created_at` (timestampz): the timestamp when it was added

#### Laws

- The `entry.amount` is never negative, the `entry.direction` determines that sign
- The `entry.amount` can never be different from either side of the `direction`, they are locked into place (source.entry.amount == destination.entry.amount)
- Every entry has a `direction` which is either `SOURCE` or `DESTINATION`
- There are only ever two entries per transaction, any `count(entries) != 2` is a bug
- A transaction entries amount are always equal (`entry.destination.amount == entry.source.amount`)

### Model: Transaction

A transaction is an event that connects two sides of a move of value where one side represents the SOURCE (source of the outflow) and DESTINATION (destination of the inflow) being represented.

Every record of inflows, and outflows are simply a representation of value moving between accounts (see [Model: Account](#model:account)) recorded in the 2 entry a transaction has. A specific transaction can never have more than 2 entries and never have only 1 or 0 entries.

And the entries specifically indicate what provides the value moved from the [Account] source to the destination [Account].

#### Laws

- A transaction has always two types of entries each with a specific `direction`:(1 `DESTINATION`, 1 `SOURCE`)
- A transaction is related to two accounts (the source and the destination).
- A transaction `entry.source` receives the `-amount` and the `entry.destination` receives the `+amount` (`entry.direction` determines the sign).
- A transaction entries must always be equal on both sides of the entries.
- A transaction `entry.amount` on both must match and one side must be SOURCE and the other DESTINATION.
- A transaction can never have entries of the same `direction`.
- A transaction can never have more than two account being represented (a transaction cant represent movement across only 1, 3 or more accounts, always just 2).
- A transaction represent an event in time for two specific `(account.account_name, account_type)`

#### Properties

- transaction_id: (UUID) representation of the specifi transaction that is being performed.
- user_id: (UUID) relevant user which transaction is related to (we really only ever have one)
- when: (timestampz) representation of `when` said transaction occured
- desc: (String) content of the event describing the transaction

### Model: Account

The system has a series of Accounts representing the different sets of system of records for a giving category of asset or liability. It can only ever be a set of:

#### Properties

- `account_id`: singular, unique ID representing the account for relationship mapping
- `user_id`: (UUID) relevant user who owns the account (we really only ever have one)
- `account_type`: INCOME (your salary or other inflow sources), EXPENSE (outflow sources), LABILITIES (considered what you owe or are owed), ASSET (singular, the wallet that represent your overall account balance)
- `account_name`: the relevant user friendly name we wish to call the accunt, generally its the category name which represent the type of asset/liability being represented (think: income, loans, leasure, etc)
- Key:
  - UNIQUE(account_name, account_type): uniquely represent a specific account to ensure no two accounts can represent the smae thing.
- Index:
  - UNIQUE INDEX(account_name, account_type): uniquely represent a specific account to ensure no two accounts can represent the smae thing.

#### Laws

- Every account has a specific `account_type` (ASSET, EXPENSE, LIABILITY, INCOME)
- Every account except the ASSET account is user generated and always from the incoming transaction log being ingested
- Every accounts `sum(account.entries.destination.amount) - sum(accounts.entries.source.amount)` = [remaining_value] tells us the accounts left over balance.

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
