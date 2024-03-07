package views

import (
	"crypto/tls"
	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"strings"
)

func NewClickhouseConn(urls, user, password, db string) (driver.Conn, error) {
	return clickhouse.Open(&clickhouse.Options{
		Addr: strings.Split(urls, ","),
		Auth: clickhouse.Auth{
			Database: db,
			Username: user,
			Password: password,
		},
		TLS: &tls.Config{
			InsecureSkipVerify: true,
		},
	})
}

//func makeQueries(ctx context.Context, conn driver.Conn, n int) uint64 {
//
//	//_, err := conn.Query(ctx, queries[n])
//	//rows, err := conn.Query(ctx, queries[0])
//	_, err := conn.Query(ctx, thisSingleQuery)
//	//fmt.Println(rows)
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	//sum += len(rows)
//
//	var sum uint64
//	//var rowCount int
//	//for rows.Next() {
//	//	rowCount++
//	//}
//	//fmt.Printf("row count: %d\n", rowCount)
//	//	var (
//	//		deviceType string
//	//		view_count uint64
//	//	)
//	//	if err := rows.Scan(
//	//		&deviceType,
//	//		&view_count,
//	//	); err != nil {
//	//		log.Fatal(err)
//	//	}
//	//	sum += view_count
//	//	log.Printf("deviceType: %s, view_count: %v", deviceType, view_count)
//	//}
//	//log.Printf("sum: %d", sum)
//	return sum
//	return 0
//}
