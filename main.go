package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/cockroachdb/cockroach-go/v2/crdb/crdbgorm"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Account is our model, which corresponds to the "accounts" database
// table.
type Account struct {
	ID      uuid.UUID `gorm:"type:uuid;default:uuid_generate_v4()"`
	Balance int
}

// Some global values, for examples
// The acctIDs global variable tracks the random acctIDs generated.
var acctIDs []uuid.UUID

// The amount to be transferred between the accounts.
const transferAmt int = 100

func transferFunds(db *gorm.DB, fromID uuid.UUID, toID uuid.UUID, amount int) error {
	var fromAccount Account
	var toAccount Account

	db.First(&fromAccount, fromID)
	db.First(&toAccount, toID)

	if fromAccount.Balance < amount {
		return fmt.Errorf("account %s balance %d is lower than transfer amount %d", fromAccount.ID, fromAccount.Balance, amount)
	}

	fromAccount.Balance -= amount
	toAccount.Balance += amount

	if err := db.Save(&fromAccount).Error; err != nil {
		return err
	}
	if err := db.Save(&toAccount).Error; err != nil {
		return err
	}
	return nil
}

func insertRows(db *gorm.DB, numRows int) error {
	// Insert rows into the "accounts" table.
	log.Printf("Creating %d new rows...", numRows)
	for i := 0; i < numRows; i++ {
		newID := uuid.New()
		newBalance := rand.Intn(10000) + transferAmt
		if err := db.Create(&Account{ID: newID, Balance: newBalance}).Error; err != nil {
			return err
		}
		acctIDs = append(acctIDs, newID)
	}
	return nil
}

func printBalances(db *gorm.DB) {
	var accounts []Account
	db.Find(&accounts)
	fmt.Printf("Balance at '%s':\n", time.Now())
	for _, account := range accounts {
		fmt.Printf("%s %d\n", account.ID, account.Balance)
	}
}

func deleteAccounts(db *gorm.DB) error {
	// Used to tear down the accounts table so we can re-run this
	// program.
	err := db.Where("id IN ?", acctIDs).Delete(Account{}).Error
	if err != nil {
		return err
	}
	return nil
}

func main() {
	// Connect to the "bank" database as the "maxroach" user.
	// Read in connection string
	scanner := bufio.NewScanner(os.Stdin)
	log.Println("Enter a connection string: ")
	scanner.Scan()
	connstring := os.ExpandEnv(scanner.Text())

	// Connect to the "bank" database
	db, err := gorm.Open(postgres.Open(connstring), &gorm.Config{
		NamingStrategy: schema.NamingStrategy{
			TablePrefix: "bank.",
		},
	})
	if err != nil {
		log.Fatal("error configuring the database: ", err)
	}

	// Automatically create the "accounts" table based on the Account
	// model.
	db.AutoMigrate(&Account{})

	// Insert five rows into the "accounts" table.
	// To handle potential transaction retry errors, we wrap the call
	// to `insertRows` in `crdbgorm.ExecuteTx`, a helper function for
	// GORM which implements a retry loop
	if err := crdbgorm.ExecuteTx(context.Background(), db, nil,
		func(tx *gorm.DB) error {
			return insertRows(db, 5)
		},
	); err != nil {
		// For information and reference documentation, see:
		//   https://www.cockroachlabs.com/docs/stable/error-handling-and-troubleshooting.html
		fmt.Println(err)
	}

	// The sequence of steps in this section is:
	// 1. Print account balances.
	// 2. Set up some Accounts and transfer funds between them inside
	// a transaction.
	// 3. Print account balances again to verify the transfer occurred.

	// Print balances before transfer.
	printBalances(db)

	fromID := acctIDs[0]
	toID := acctIDs[0:][rand.Intn(len(acctIDs))]

	// Transfer funds between accounts.  To handle potential
	// transaction retry errors, we wrap the call to `transferFunds`
	// in `crdbgorm.ExecuteTx`, a helper function for GORM which
	// implements a retry loop
	if err := crdbgorm.ExecuteTx(context.Background(), db, nil,
		func(tx *gorm.DB) error {
			return transferFunds(tx, fromID, toID, transferAmt)
		},
	); err != nil {
		// For information and reference documentation, see:
		//   https://www.cockroachlabs.com/docs/stable/error-handling-and-troubleshooting.html
		fmt.Println(err)
	}

	// Print balances after transfer to ensure that it worked.
	printBalances(db)

	// Delete accounts so we can start fresh when we want to run this
	// program again.
	deleteAccounts(db)
}
